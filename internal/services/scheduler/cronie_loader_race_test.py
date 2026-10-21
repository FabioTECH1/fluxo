"""Build a loader-only fixture from Ubuntu's fully patched Cronie 1.7.1 source.

Usage: python3 cronie_loader_race_test.py /patched/configured/source /output/fixture
Run as root in an empty scratch directory with argument polling or inotify.
No scheduler daemon starts and no parsed command is executed. The fixture keeps
the real parser, loader, mtime cache, inotify event reads and watch installation.
Only paths, time, directory ordering and an in-scan mutation hook are controlled.
Source hashes and the complete instrumentation diff are retained beside it.
"""
import difflib
import hashlib
import pathlib
import shutil
import subprocess
import sys

source, output = [pathlib.Path(p).resolve() for p in sys.argv[1:3]]
work = output.with_suffix('.source')
work.mkdir()
names = ['bitstring.h', 'entry.c', 'env.c', 'externs.h', 'funcs.h', 'globals.h',
         'macros.h', 'misc.c', 'pathnames.h', 'pw_dup.c', 'structs.h', 'user.c',
         'database.c', 'cron.c']
for name in names:
    shutil.copyfile(source / 'src' / name, work / name)
shutil.copyfile(source / 'config.h', work / 'config.h')
(work / 'source-sha256.txt').write_text(''.join(
    hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n'
    for p in sorted(work.iterdir()) if p.is_file()))
# This is the generated packaging-path header's shape, with scratch paths.
(work / 'cron-paths.h').write_text('''
#define SPOOL_DIR "spool"
#define SYSCRONTAB "system"
#define SYS_CROND_DIR "cron.d"
#define CRON_HOSTNAME ".cron.hostname"
#define CRON_ALLOW "cron.allow"
#define CRON_DENY "cron.deny"
#define SYSCONFDIR "."
''')
diffs = []
original = (work / 'database.c').read_text()
changed = original.replace('#include "pathnames.h"', '''#include "pathnames.h"
/* Test-only order/time/in-scan publication; ownership and parser stay real. */
DIR *fixture_opendir(const char *);
struct dirent *fixture_readdir(DIR *);
void fixture_after_file(cron_db *, const char *);
#define opendir fixture_opendir
#define readdir fixture_readdir''')
needle = 'process_crontab("root", NULL, tabname, &new_db, old_db);'
assert changed.count(needle) == 2
changed = changed.replace(needle, needle + '\n\t\t\tfixture_after_file(&new_db, dp->d_name);')
diffs.extend(difflib.unified_diff(original.splitlines(True), changed.splitlines(True),
                               fromfile='database.c', tofile='database.c.fixture'))
(work / 'database.c').write_text(changed)
# Compile exact watch functions, discarding the daemon/main and all other code.
original = (work / 'cron.c').read_text()
begin = original.index('static void reset_watches(void)')
end = original.index('\n#endif', begin)
watch = original[begin:end]
(work / 'watch.c').write_text('''#include "config.h"
#include <errno.h>
#include <unistd.h>
#include <sys/inotify.h>
#include "funcs.h"
#include "globals.h"
#include "pathnames.h"
int inotify_enabled;
int wd[3];
const char *watchpaths[3] = {SPOOL_DIR, SYS_CROND_DIR, SYSCRONTAB};
''' + watch)
(work / 'instrumentation.diff').write_text(''.join(diffs))
(work / 'fixture.c').write_text(r'''
#include "config.h"
#define MAIN_PROGRAM
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <assert.h>
#include <dirent.h>
#include <sys/stat.h>
#include <sys/inotify.h>
#include <utime.h>
#include "funcs.h"
#include "globals.h"
#include "pathnames.h"
static time_t clock_now = 1000000002;
static int position, injected, mutate = 1;
static ino_t cron_inode;
static const char *names[] = {"legacy", "marker", "custom", "new", NULL};
time_t fixture_time(time_t *t) { if(t) *t=clock_now; return clock_now; }
static int contains(cron_db *db, const char *cmd) {
    user *u; entry *e;
    for(u=db->head;u;u=u->next) for(e=u->crontab;e;e=e->next)
        if(!strcmp(e->cmd,cmd)) return 1;
    return 0;
}
static void stamp(const char *path,time_t at) { struct utimbuf t={at,at}; assert(!utime(path,&t)); }
static void publish(const char *path,const char *cmd,time_t at) {
    FILE *f=fopen(path,"w"); assert(f);
    assert(fprintf(f,"* * * * * root %s\n",cmd)>0); assert(!fclose(f));
    assert(!chmod(path,0644)); stamp(path,at);
}
DIR *fixture_opendir(const char *path) {
    if(!strcmp(path,SYS_CROND_DIR)) position=0;
    return opendir(path);
}
struct dirent *fixture_readdir(DIR *dir) {
    struct stat st; assert(!fstat(dirfd(dir),&st));
    if(st.st_ino!=cron_inode) return readdir(dir);
    static struct dirent item;
    if(!names[position]) return NULL;
    memset(&item,0,sizeof(item)); strcpy(item.d_name,names[position++]); return &item;
}
void fixture_after_file(cron_db *db,const char *name) {
    if(mutate && !strcmp(name,"legacy")) {
        assert(contains(db,"LEGACY"));
        publish("cron.d/marker","MARKER_A",clock_now);
        assert(!unlink("cron.d/legacy")); injected=1; mutate=0;
    }
}
int main(int argc,char **argv) {
    assert(geteuid()==0 && argc==2);
    int events=!strcmp(argv[1],"inotify");
    assert(events || !strcmp(argv[1],"polling"));
    ProgramName="fluxo-cronie-loader";
    assert(!mkdir("cron.d",0755)); assert(!mkdir("spool",0700));
    struct stat st; assert(!stat("cron.d",&st)); cron_inode=st.st_ino;
    FILE *f=fopen("system","w"); assert(f); assert(!fclose(f)); stamp("system",clock_now-1);
    publish("cron.d/legacy","LEGACY",clock_now-1);
    publish("cron.d/marker","MARKER_OLD",clock_now-1);
    publish("cron.d/custom","CUSTOM",clock_now-1);
    publish("cron.d/new","NEW_MANAGED",clock_now-1);
    stamp("spool",clock_now-1); stamp("cron.d",clock_now-1);
    cron_db db={0}; db.ifd=inotify_init1(IN_CLOEXEC|IN_NONBLOCK); assert(db.ifd>=0);
    set_cron_watched(db.ifd);
    if(events) { publish("cron.d/new","NEW_MANAGED",clock_now-1); check_inotify_database(&db); }
    else assert(load_database(&db));
    assert(injected && contains(&db,"LEGACY") && contains(&db,"MARKER_A"));
    assert(!contains(&db,"MARKER_OLD") && contains(&db,"CUSTOM") && contains(&db,"NEW_MANAGED"));
    puts("PASS A: completed real loader contains deleted legacy command together with marker A");
    publish("cron.d/marker","MARKER_B",clock_now);
    stamp("cron.d",clock_now-1); /* unchanged directory mtime */
    if(events) check_inotify_database(&db); else load_database(&db);
    assert(contains(&db,"MARKER_A") && !contains(&db,"MARKER_B"));
    puts("PASS cache: same-second B cannot be inferred from publication or event delivery");
    ++clock_now; publish("cron.d/marker","MARKER_B",clock_now);
    stamp("cron.d",clock_now-2);
    if(events) check_inotify_database(&db); else assert(load_database(&db));
    assert(!contains(&db,"LEGACY") && !contains(&db,"MARKER_A") && contains(&db,"MARKER_B"));
    assert(contains(&db,"CUSTOM") && contains(&db,"NEW_MANAGED"));
    assert(access("cron.d/legacy",F_OK)!=0);
    puts("PASS B: later authenticated B requires a real reload and retires old data without recreating deleted work");
    if(events) {
        set_cron_unwatched(db.ifd); /* watch loss, queued IN_IGNORED; real function reinstates watches */
        check_inotify_database(&db);
        ++clock_now; publish("cron.d/marker","MARKER_C",clock_now);
        check_inotify_database(&db); assert(contains(&db,"MARKER_C"));
        puts("PASS watch: actual Cronie watch installation recovers after watch loss");
    }
    close(db.ifd); return 0;
}
''')
subprocess.run(['cc', '-O0', '-g', '-Dtime=fixture_time', '-ffunction-sections',
                '-fdata-sections', '-I.', '-o', str(output), 'fixture.c', 'watch.c',
                'database.c', 'user.c', 'entry.c', 'env.c', 'misc.c', 'pw_dup.c',
                '-Wl,--gc-sections'], cwd=work, check=True)
print('Built loader-only fixture:', output, 'source evidence:', work)
