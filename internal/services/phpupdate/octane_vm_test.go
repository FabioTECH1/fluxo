package phpupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

func vmApplicationFile(t *testing.T, path, data string) {
	t.Helper()
	old, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	t.Cleanup(func() {
		if existed {
			_ = os.WriteFile(path, old, info.Mode().Perm())
		} else {
			_ = os.Remove(path)
		}
	})
	if err = os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cred, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(path, int(cred.Uid), int(cred.Gid)); err != nil {
		t.Fatal(err)
	}
}

func TestVMOctaneConfigurationInspectionDoesNotBootProviders(t *testing.T) {
	requireRecoveryVM(t)
	if err := octaneruntime.Install(); err != nil {
		t.Fatal(err)
	}
	vmApplicationFile(t, filepath.Join(recoveryVMApp, ".rr.yaml"), "")
	vmApplicationFile(t, filepath.Join(recoveryVMApp, "config/octane.php"), "<?php return ['server'=>'swoole'];\n")
	provider := filepath.Join(recoveryVMApp, "app/Providers/RecoveryInspectionProvider.php")
	sentinel := filepath.Join(recoveryVMApp, "storage/app/provider-booted")
	_ = os.Remove(sentinel)
	t.Cleanup(func() { _ = os.Remove(sentinel) })
	vmApplicationFile(t, provider, `<?php
namespace App\Providers;
class RecoveryInspectionProvider extends \Illuminate\Support\ServiceProvider {
 public function register(): void {
  file_put_contents(storage_path('app/provider-booted'), 'provider ran');
  throw new \RuntimeException('Acceptance database is deliberately unavailable');
 }
}`)
	vmApplicationFile(t, filepath.Join(recoveryVMApp, "bootstrap/providers.php"), "<?php return [App\\Providers\\AppServiceProvider::class, App\\Providers\\RecoveryInspectionProvider::class];\n")
	cache := filepath.Join(recoveryVMApp, "bootstrap/cache/config.php")
	vmApplicationFile(t, cache, "<?php return [];\n")
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	c := Consumer{Name: "configuration acceptance", Directory: recoveryVMApp}
	first, err := probeOctane(context.Background(), "8.4", c, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("uncached inspection booted application provider")
	}
	// Generate only the configuration cache, without invoking config:cache (which
	// intentionally boots a real console application and would run the provider).
	loader := strings.Split(strings.TrimPrefix(octaneProbe, "<?php"), "$version =")[0]
	loader += `file_put_contents($app->getCachedConfigPath(), '<?php return '.var_export($app['config']->all(),true).';'); ob_end_clean();`
	if _, err = syscmd.RunApplicationProbe(context.Background(), 15*time.Second, "fluxo", recoveryVMApp, "/usr/bin/php8.4", "-r", loader); err != nil {
		t.Fatal(err)
	}
	second, err := probeOctane(context.Background(), "8.4", c, false)
	if err != nil {
		t.Fatal(err)
	}
	if digest(first.Descriptor) != digest(second.Descriptor) {
		t.Fatal("cached and uncached Octane descriptors differ")
	}
	if _, err = os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("cached inspection booted application provider")
	}
	// Establish that the sentinel is effective: normal console bootstrap reaches
	// this provider and fails, while the production inspection above did not.
	if err = os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	_, err = syscmd.RunApplicationProbe(context.Background(), 15*time.Second, "fluxo", recoveryVMApp, "/usr/bin/php8.4", "-r", `require 'vendor/autoload.php';$app=require 'bootstrap/app.php';$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();`)
	if err == nil {
		t.Fatal("provider fixture did not reject ordinary console boot")
	}
	if _, err = os.Stat(sentinel); err != nil {
		t.Fatal("provider fixture was not reached")
	}
}

func vmOctaneReady(t *testing.T, c Consumer) unitSnapshot {
	t.Helper()
	var last error
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		u, err := unitState(context.Background(), unitName(c.ID, 1))
		if err == nil && u.Active {
			u.Octane, err = inspectOctaneProcesses(context.Background(), "8.4", c, u)
			if err == nil {
				err = octaneHealth(context.Background(), "8.4", c, u)
			}
			if err == nil {
				return u
			}
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Octane did not become verifiably ready: %v\n%s", last, vmRun(t, "/usr/bin/journalctl", "-u", unitName(c.ID, 1), "-n", "30", "--no-pager"))
	return unitSnapshot{}
}

func vmNativeRepairHead(t *testing.T, g *recoveryGeneration) {
	t.Helper()
	old := g.Head
	if err := finish(old, "interrupted", "Acceptance signal boundary"); err != nil {
		t.Fatal(err)
	}
	g.Head = uuid.NewString()
	if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,phase,message,created_at,updated_at) VALUES (?,'repair','8.4','running','draining','Acceptance repair',?,?)`, g.Head, time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g)
	if _, err := database.DB.Exec("UPDATE php_update_generations SET head_operation_id=?,payload=? WHERE id=? AND head_operation_id=?", g.Head, string(raw), g.ID, old); err != nil {
		t.Fatal(err)
	}
}

func TestVMNativeOctaneDrainAndRepeatedRepair(t *testing.T) {
	requireRecoveryVM(t)
	if err := octaneruntime.Install(); err != nil {
		t.Fatal(err)
	}
	for index, backend := range []string{"swoole", "roadrunner"} {
		t.Run(backend, func(t *testing.T) {
			m := testStore(t)
			cache := filepath.Join(recoveryVMApp, "bootstrap/cache/config.php")
			vmApplicationFile(t, cache, "<?php return [];\n")
			if err := os.Remove(cache); err != nil {
				t.Fatal(err)
			}
			vmApplicationFile(t, filepath.Join(recoveryVMApp, ".rr.yaml"), "")
			vmApplicationFile(t, filepath.Join(recoveryVMApp, "config/octane.php"), fmt.Sprintf(`<?php return ['server'=>'%s','swoole'=>['options'=>['max_wait_time'=>15,'worker_num'=>1,'task_worker_num'=>1]],'max_execution_time'=>30];`, backend))
			vmApplicationFile(t, filepath.Join(recoveryVMApp, "routes/web.php"), `<?php
use Illuminate\Support\Facades\Route;
Route::get('/', fn () => 'healthy');
Route::get('/recovery-slow/{label}', function ($label) {
 file_put_contents(storage_path('app/'.$label.'-started'), 'started');
 $until=microtime(true)+6; while(microtime(true)<$until) { usleep(50000); }
 file_put_contents(storage_path('app/'.$label.'-finished'), 'finished');
 return 'finished naturally';
});
Route::get('/admission/{label}', function ($label) {
 file_put_contents(storage_path('app/admission-'.$label), 'entered Laravel');
 return 'admitted';
});`)
			c := Consumer{ID: 9102000 + index, Name: "Native " + backend, Kind: "laravel_octane", Directory: recoveryVMApp, Instances: 1, Port: 18877 + index}
			c.Command = octaneruntime.Command("8.4", c.Port)
			probe, err := probeOctane(context.Background(), "8.4", c, false)
			if err != nil {
				t.Fatal(err)
			}
			c.Octane = &probe.Descriptor
			name := unitName(c.ID, 1)
			vmUnit(t, name, "/usr/bin/"+c.Command, c.Directory)
			// A stopped Octane unit remains stopped even through the full barrier lifecycle.
			stopped, err := unitState(context.Background(), name)
			if err != nil {
				t.Fatal(err)
			}
			g := vmGeneration(t, m, []unitSnapshot{stopped})
			bounded, cancelDrain := context.WithTimeout(context.Background(), 25*time.Second)
			err = m.drainConsumers(bounded, &g)
			cancelDrain()
			if err != nil {
				t.Fatal(err)
			}
			if err = m.startConsumers(context.Background(), &g); err != nil {
				t.Fatal(err)
			}
			if state, e := unitState(context.Background(), name); e != nil || state.Active {
				t.Fatal("stopped Octane was started", e)
			}
			if err = m.releaseBarriers(context.Background(), g); err != nil {
				t.Fatal(err)
			}
			if err = finish(g.Head, "succeeded", "stopped fixture complete"); err != nil {
				t.Fatal(err)
			}
			vmRun(t, "/usr/bin/systemctl", "start", name)
			u := vmOctaneReady(t, c)
			g = vmGeneration(t, m, []unitSnapshot{u})
			label := "native-" + uuid.NewString()
			started := filepath.Join(recoveryVMApp, "storage/app", label+"-started")
			finished := filepath.Join(recoveryVMApp, "storage/app", label+"-finished")
			t.Cleanup(func() { _ = os.Remove(started); _ = os.Remove(finished) })
			response := make(chan error, 1)
			go func() {
				client := http.Client{Timeout: 25 * time.Second}
				r, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/recovery-slow/%s", c.Port, label))
				if e == nil {
					data, readErr := io.ReadAll(r.Body)
					r.Body.Close()
					e = readErr
					if e == nil && string(data) != "finished naturally" {
						e = fmt.Errorf("unexpected slow response: %s", data)
					}
				}
				response <- e
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err = os.Stat(started); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("slow request never started")
				}
				time.Sleep(50 * time.Millisecond)
			}
			// Queue requests behind the occupied synchronous Swoole worker. Their
			// callbacks have not been admitted when shutdown closes the shared gate.
			var burst chan struct{}
			if backend == "swoole" {
				burst = make(chan struct{}, 20)
				for index := 0; index < 20; index++ {
					entry := fmt.Sprintf("%s-%d", label, index)
					path := filepath.Join(recoveryVMApp, "storage/app", "admission-"+entry)
					t.Cleanup(func() { _ = os.Remove(path) })
					go func() {
						client := http.Client{Timeout: 15 * time.Second}
						r, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/admission/%s", c.Port, entry))
						if e == nil {
							r.Body.Close()
						}
						burst <- struct{}{}
					}()
				}
				time.Sleep(50 * time.Millisecond)
			}
			// Simulate interruption after durable signal intent, before delivery.
			if err = m.prepareBarrier(context.Background(), g, u); err != nil {
				t.Fatal(err)
			}
			b, err := getBarrier(name)
			if err != nil {
				t.Fatal(err)
			}
			b.State = "stopping"
			if backend == "swoole" {
				b.NativeSignalHead = g.Head
				b.NativeDelivery = "intent"
			}
			if err = saveBarrier(b); err != nil {
				t.Fatal(err)
			}
			vmNativeRepairHead(t, &g)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err = m.drainConsumers(ctx, &g)
			cancel()
			if err == nil {
				t.Fatal("in-flight request was not retained during initial drain")
			}
			if _, err = os.Stat(finished); err == nil {
				t.Fatal("fixture request ended before interruption check")
			}
			b, err = getBarrier(name)
			if err != nil || b.NativeDelivery != "sent" {
				t.Fatalf("native signal was not durably delivered before interruption: %+v %v", b, err)
			}
			// Simulate another reviewed Repair immediately after native delivery.
			// Only the native master may receive it; Artisan TERM would SIGKILL workers.
			vmNativeRepairHead(t, &g)
			bounded, cancelDrain = context.WithTimeout(context.Background(), 25*time.Second)
			err = m.drainConsumers(bounded, &g)
			cancelDrain()
			if err != nil {
				t.Fatal(err)
			}
			if err = <-response; err != nil {
				t.Fatal("native repeated Repair interrupted request", err)
			}
			if _, err = os.Stat(finished); err != nil {
				t.Fatal("request did not finish naturally", err)
			}
			if burst != nil {
				for index := 0; index < 20; index++ {
					<-burst
				}
				for index := 0; index < 20; index++ {
					if _, e := os.Stat(filepath.Join(recoveryVMApp, "storage/app", fmt.Sprintf("admission-%s-%d", label, index))); !os.IsNotExist(e) {
						t.Fatal("queued request entered Laravel after maintenance admission closed", index, e)
					}
				}
			}
			if err = m.startConsumers(context.Background(), &g); err != nil {
				t.Fatal(err)
			}
			vmOctaneReady(t, c)
			if err = m.releaseBarriers(context.Background(), g); err != nil {
				t.Fatal(err)
			}
			t.Log("Native signal intent/delivery interruption, repeated Repair, full request drain, restart and stopped-state preservation passed")
		})
	}
}

func TestVMSwooleQueuedTasksAndExistingRequestDeadline(t *testing.T) {
	requireRecoveryVM(t)
	if err := octaneruntime.Install(); err != nil {
		t.Fatal(err)
	}
	for index, kind := range []string{"queued_tasks", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			m := testStore(t)
			cache := filepath.Join(recoveryVMApp, "bootstrap/cache/config.php")
			vmApplicationFile(t, cache, "<?php return [];\n")
			if err := os.Remove(cache); err != nil {
				t.Fatal(err)
			}
			limit := 30
			if kind == "deadline" {
				limit = 1
			}
			vmApplicationFile(t, filepath.Join(recoveryVMApp, "config/octane.php"), fmt.Sprintf(`<?php return ['server'=>'swoole','swoole'=>['options'=>['max_wait_time'=>15,'worker_num'=>1,'task_worker_num'=>1]],'max_execution_time'=>%d];`, limit))
			vmApplicationFile(t, filepath.Join(recoveryVMApp, "routes/web.php"), `<?php
use Illuminate\Support\Facades\Route;
use Laravel\Octane\Facades\Octane;
Route::get('/', fn () => 'healthy');
Route::get('/recovery-task/{label}', function ($label) {
 $work = static function ($suffix) use ($label) {
  file_put_contents(storage_path('app/'.$label.$suffix.'-started'), 'started');
  $until=microtime(true)+3;while(microtime(true)<$until){usleep(50000);}
  file_put_contents(storage_path('app/'.$label.$suffix.'-finished'), 'finished');
  return 'finished naturally';
 };
 $first=fn()=>$work('a');
 $second=fn()=>$work('b');
 $result=Octane::concurrently([$first,$second],20000);
 return implode(',', $result);
});
Route::get('/recovery-slow/{label}', function ($label) {
 file_put_contents(storage_path('app/'.$label.'-started'), 'started');
 $until=microtime(true)+6;while(microtime(true)<$until){usleep(50000);}
 file_put_contents(storage_path('app/'.$label.'-finished'), 'finished');
 return 'finished naturally';
});`)
			c := Consumer{ID: 9103000 + index, Name: "Swoole " + kind, Kind: "laravel_octane", Directory: recoveryVMApp, Instances: 1, Port: 18879 + index}
			c.Command = octaneruntime.Command("8.4", c.Port)
			probe, err := probeOctane(context.Background(), "8.4", c, false)
			if err != nil {
				t.Fatal(err)
			}
			c.Octane = &probe.Descriptor
			name := unitName(c.ID, 1)
			vmUnit(t, name, "/usr/bin/"+c.Command, c.Directory)
			vmRun(t, "/usr/bin/systemctl", "start", name)
			u := vmOctaneReady(t, c)
			g := vmGeneration(t, m, []unitSnapshot{u})
			label := "runtime-" + uuid.NewString()
			suffix := ""
			path := "recovery-slow"
			if kind == "queued_tasks" {
				suffix = "a"
				path = "recovery-task"
			}
			started := filepath.Join(recoveryVMApp, "storage/app", label+suffix+"-started")
			t.Cleanup(func() {
				for _, s := range []string{"", "a", "b"} {
					for _, e := range []string{"started", "finished"} {
						_ = os.Remove(filepath.Join(recoveryVMApp, "storage/app", label+s+"-"+e))
					}
				}
			})
			type outcome struct {
				body   string
				status int
				err    error
			}
			response := make(chan outcome, 1)
			begin := time.Now()
			go func() {
				client := http.Client{Timeout: 25 * time.Second}
				r, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/%s/%s", c.Port, path, label))
				result := outcome{err: e}
				if e == nil {
					b, readErr := io.ReadAll(r.Body)
					r.Body.Close()
					result = outcome{body: string(b), status: r.StatusCode, err: readErr}
				}
				response <- result
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err = os.Stat(started); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fixture work never began")
				}
				time.Sleep(25 * time.Millisecond)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			err = m.drainConsumers(ctx, &g)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			result := <-response
			if kind == "queued_tasks" {
				if result.err != nil || result.status != 200 || result.body != "finished naturally,finished naturally" {
					t.Fatalf("queued tasks did not finish: %+v", result)
				}
				for _, s := range []string{"a", "b"} {
					if _, err = os.Stat(filepath.Join(recoveryVMApp, "storage/app", label+s+"-finished")); err != nil {
						t.Fatal("task did not finish", s, err)
					}
				}
			} else {
				if time.Since(begin) > 5*time.Second {
					t.Fatal("maintenance disabled or reset the existing request watchdog deadline")
				}
				if _, err = os.Stat(filepath.Join(recoveryVMApp, "storage/app", label+"-finished")); !os.IsNotExist(err) {
					t.Fatal("over-deadline fixture unexpectedly completed")
				}
				if result.err == nil && result.status != 408 {
					t.Fatalf("unexpected watchdog outcome: %+v", result)
				}
			}
			if err = m.startConsumers(context.Background(), &g); err != nil {
				t.Fatal(err)
			}
			vmOctaneReady(t, c)
			if err = m.releaseBarriers(context.Background(), g); err != nil {
				t.Fatal(err)
			}
		})
	}
}
