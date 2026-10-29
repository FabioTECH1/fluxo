package nodeupdate

import (
	"context"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
)

func TestScheduledTaskReviewBindsExactGuardedContracts(t *testing.T) {
	testManager(t)
	for i,cmd:=range []string{maintenancetask.PackageCleanupCommand,maintenancetask.CertificateRenewalCommand,maintenancetask.ComposerUpdateCommand} {
		if _,err:=database.DB.Exec("INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(?,0,'independent task','0 0 * * 0',?,'root')",i+1,cmd);err!=nil{t.Fatal(err)}
	}
	called:=0
	rootAdmission:=func(_ context.Context,c maintenancetask.Contract,db string)(maintenancetask.Receipt,error){called++;return maintenancetask.Receipt{Contract:c,Identity:"bound",Database:db,Helper:"verified",Profile:"fixed-dispatch-v1"},nil}
	reasons,tasks,err:=inspectCronInventoryDetailed(context.Background(),func(context.Context,int,scheduler.Entry)error{t.Fatal("root job became a Laravel scheduler");return nil},rootAdmission)
	if err!=nil||len(reasons)!=0||len(tasks)!=3||called!=3{t.Fatal(reasons,tasks,called,err)}
	a,b:=Impact{ScheduledTasks:tasks},Impact{ScheduledTasks:append([]maintenancetask.Receipt(nil),tasks...)}
	if !sameConsumers(a,b){t.Fatal("identical guarded contracts changed")}
	b.ScheduledTasks[0].Contract.Expression="1 1 * * *"
	if sameConsumers(a,b){t.Fatal("review ignored a changed schedule")}
	if !sameOriginalConsumers(a,Impact{}){t.Fatal("fresh reviewed additions rewrote or blocked immutable historical intent")}
}
