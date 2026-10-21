"""Build a deterministic loader-only race fixture from qualified Ubuntu cron source.

Usage: python3 legacy_loader_race_test.py /path/to/patched/cron/source /output/fixture
Run the resulting executable as root in an empty scratch directory on the test VM.
It parses only local fixture files and never starts cron or executes a command.
The source copy, instrumentation diff and source hashes are retained beside it.
"""
import difflib
import hashlib
import pathlib
import shutil
import subprocess
import sys

source, output = map(lambda p: pathlib.Path(p).resolve(), sys.argv[1:3])
work = output.with_suffix('.source')
work.mkdir()
for name in ['cron.h','compat.h','config.h','externs.h','pathnames.h','bitstring.h','database.c','user.c','entry.c','env.c','misc.c','compat.c']:
    shutil.copyfile(source/name, work/name)
(work/'source-sha256.txt').write_text(''.join(hashlib.sha256((source/n).read_bytes()).hexdigest()+'  '+n+'\n' for n in sorted(p.name for p in work.iterdir() if p.is_file())))
diffs=[]
for name in ['pathnames.h','database.c']:
    original=(work/name).read_text(); changed=original
    if name=='pathnames.h':
        changed=changed.replace('"crontabs"','"spool"').replace('"/etc/crontab"','"system"').replace('"/etc/cron.d"','"cron.d"')
    else:
        changed=changed.replace('#include <time.h>','#include <time.h>\n/* Fluxo test-only deterministic enumeration and in-scan mutation. */\nstruct dirent *fixture_readdir(DIR *);\nvoid fixture_after_file(cron_db *, const char *);')
        changed=changed.replace('(dp = readdir(dir))','(dp = fixture_readdir(dir))',1)
        needle='&statbuf, &new_db, old_db);'
        assert changed.count(needle)==2
        changed=changed.replace(needle,needle+'\n\t\tfixture_after_file(&new_db, dp->d_name);',1)
    diffs.extend(difflib.unified_diff(original.splitlines(True),changed.splitlines(True),fromfile=name,tofile=name+'.fixture'))
    (work/name).write_text(changed)
(work/'instrumentation.diff').write_text(''.join(diffs))
(work/'fixture.c').write_text(r'''
#define MAIN_PROGRAM
#include "cron.h"
#include <assert.h>
#include <utime.h>
#include <sys/stat.h>
static int pass=0, position=0, injected=0;
static const char *names[]={"legacy","marker","custom","new",NULL};
static int contains(cron_db *db,const char *cmd){user *u;entry *e;for(u=db->head;u;u=u->next)for(e=u->crontab;e;e=e->next)if(!strcmp(e->cmd,cmd))return 1;return 0;}
static void publish(const char *path,const char *cmd,time_t stamp){FILE*f=fopen(path,"w");assert(f);assert(fprintf(f,"* * * * * root %s\n",cmd)>0);assert(!fclose(f));assert(!chmod(path,0644));struct utimbuf t={stamp,stamp};assert(!utime(path,&t));}
struct dirent *fixture_readdir(DIR *dir){static struct dirent item;(void)dir;if(!names[position])return NULL;memset(&item,0,sizeof(item));strcpy(item.d_name,names[position++]);return &item;}
void fixture_after_file(cron_db *db,const char *name){if(pass==0&&!strcmp(name,"legacy")){assert(contains(db,"LEGACY"));publish("cron.d/marker","MARKER_A",1000000002);assert(!unlink("cron.d/legacy"));injected=1;}}
int main(void){assert(geteuid()==0);ProgramName="fluxo-loader-race";assert(!mkdir("cron.d",0755));assert(!mkdir("spool",0700));FILE*f=fopen("system","w");assert(f);fclose(f);publish("cron.d/legacy","LEGACY",1000000001);publish("cron.d/marker","MARKER_OLD",1000000001);publish("cron.d/custom","CUSTOM",1000000001);publish("cron.d/new","NEW_MANAGED",1000000001);cron_db db={0};load_database(&db);assert(injected);assert(contains(&db,"LEGACY"));assert(contains(&db,"MARKER_A"));assert(!contains(&db,"MARKER_OLD"));assert(contains(&db,"CUSTOM"));assert(contains(&db,"NEW_MANAGED"));puts("PASS A: deleted legacy command and marker A coexist in the real loader's completed database");publish("cron.d/marker","MARKER_B",1000000003);struct utimbuf same={db.sysd_mtime,db.sysd_mtime};assert(!utime("cron.d",&same));pass=1;position=0;load_database(&db);assert(!contains(&db,"LEGACY"));assert(!contains(&db,"MARKER_A"));assert(contains(&db,"MARKER_B"));assert(contains(&db,"CUSTOM"));assert(contains(&db,"NEW_MANAGED"));assert(access("cron.d/legacy",F_OK)!=0);puts("PASS B: individual-file reload loads B, removes legacy/A, preserves custom/new schedules and deletion");return 0;}
''')
subprocess.run(['cc','-O0','-g','-DDEBUGGING=0','-DPOSIX','-ffunction-sections','-fdata-sections','-I.', '-o',str(output),'fixture.c','database.c','user.c','entry.c','env.c','misc.c','compat.c','-Wl,--gc-sections'],cwd=work,check=True)
print('Built loader-only fixture:',output,'source evidence:',work)
