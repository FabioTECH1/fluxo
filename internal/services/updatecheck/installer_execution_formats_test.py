"""Execute the actual early installer format policy against isolated records."""
import copy
import json
import pathlib
import re
import sys
import unittest
import uuid

INSTALLER = pathlib.Path(__file__).resolve().parents[3] / 'install.sh'
program = INSTALLER.read_text().split("<<'PY_EXECUTION_FORMATS'\n", 1)[1].split('\nPY_EXECUTION_FORMATS', 1)[0]
# The protected reader itself is exercised by native qualification. Replace only
# its I/O with an in-memory map; execute the exact production policy unchanged.
policy = compile(program[program.index('try:\n    raw_floor='):], str(INSTALLER), 'exec')
FLOOR = '/var/lib/fluxo/execution-readers.json'
SCHEDULER = '/var/lib/fluxo-scheduler/state.json'
TASKS = '/var/lib/fluxo-system-tasks/state.json'


def floor():
    return dict(format=1, phase='committed', scheduler_support=5, system_task_support=3,
                scheduler_schema=4, system_task_schema=3, adapters=['systemd-cgroup-exit-v1','systemd-persistent-v1'],
                role_contracts={'scheduler-execute': ['systemd-cgroup-exit-v1'],
                                'system-task-execute': ['systemd-cgroup-exit-v1'],
                                'system-task-profile': ['apt-a0-noqueue-v3'], 'scheduler-backend-reader':['ubuntu-noble-cron-v1','cronie-noble-1','ubuntu-jammy-cron-v1','ubuntu-resolute-cron-v1']},
                activation_id='1af605c7-82ca-4245-9fc5-1240cae7d8ab',
                installation_id='a'*32, binary_sha256='b'*64, plan_sha256='c'*64)


def core(schema, adapter='systemd-cgroup-exit-v1'):
    return dict(schema=schema, entries={}, invocations={'history': dict(adapter=adapter)})


class ExecutionFormatPolicy(unittest.TestCase):
    def run_policy(self, records, accepted):
        def read(path, limit):
            return json.dumps(records[path]).encode() if path in records else None
        environment = dict(read=read, json=json, re=re, sys=sys, uuid=uuid)
        if accepted:
            exec(policy, environment)
        else:
            with self.assertRaises(SystemExit):
                exec(policy, environment)

    def test_fresh_and_historical(self):
        self.run_policy({}, True)
        self.run_policy({SCHEDULER: core(2, ''), TASKS: core(1, '')}, True)

    def test_partial_conversion_cannot_enter_provisioning(self):
        for schema, path in [(3, SCHEDULER), (2, TASKS)]:
            self.run_policy({path: core(schema)}, False)
        prepared = floor()
        prepared['phase'] = 'prepared'
        self.run_policy({FLOOR: prepared, SCHEDULER: core(3)}, False)

    def test_exact_committed_contract(self):
        self.run_policy({FLOOR: floor(), SCHEDULER: core(4), TASKS: core(3)}, True)
        for mutate in [
                lambda f: f.update(phase='unknown'),
                lambda f: f.update(scheduler_support=7),
                lambda f: f.update(scheduler_support=4),
                lambda f: f.update(installation_id='bad'),
                lambda f: f.update(activation_id='../other'),
                lambda f: f.update(format=True),
                lambda f: f.update(unknown_contract=True),
                lambda f: f.update(role_contracts={}),
                lambda f: f['role_contracts'].update({'system-task-profile': ['legacy-host-inputs-v1']}),
                lambda f: f['role_contracts'].update({'future-profile': ['unknown']}),
                lambda f: f.update(adapters=['systemd-persistent-249-v1'])]:
            record = copy.deepcopy(floor())
            mutate(record)
            self.run_policy({FLOOR: record, SCHEDULER: core(4), TASKS: core(3)}, False)

    def test_schema5_requires_explicit_loader_and_bootstrap_contracts(self):
        active = floor()
        active.update(scheduler_schema=5, scheduler_support=6)
        active['role_contracts']['scheduler-loader-reader']=['exact-entry-loader-epoch-v1']
        active['role_contracts']['scheduler-execute'] += ['exact-entry-loader-admission-v1','scheduler-bootstrap-drop-exec-v1']
        current = core(5)
        current['loader_epochs_established']=False
        self.run_policy({FLOOR:active,SCHEDULER:current,TASKS:core(3)},True)
        # Historical4 stays a supported read contract, independent of the
        # compiled generation. New schema5 cannot borrow its weaker floor.
        self.run_policy({FLOOR:floor(),SCHEDULER:core(4),TASKS:core(3)},True)
        for missing in ['scheduler-loader-reader','scheduler-execute']:
            broken=copy.deepcopy(active)
            del broken['role_contracts'][missing]
            self.run_policy({FLOOR:broken,SCHEDULER:current,TASKS:core(3)},False)
        for established in [None, 0, 'false']:
            broken=copy.deepcopy(current)
            broken['loader_epochs_established']=established
            self.run_policy({FLOOR:active,SCHEDULER:broken,TASKS:core(3)},False)
        broken=copy.deepcopy(active)
        broken['scheduler_support']=5
        self.run_policy({FLOOR:broken,SCHEDULER:current,TASKS:core(3)},False)
        self.run_policy({FLOOR:floor(),SCHEDULER:current,TASKS:core(3)},False)

    def test_creation_marker_and_partial_journal_refusal(self):
        for path in ['/var/lib/fluxo-scheduler.initialized','/var/lib/fluxo-system-tasks.initialized','/var/lib/fluxo-system-tasks.lock','/var/lib/fluxo-scheduler']:
            self.run_policy({path: 'activation:1af605c7-82ca-4245-9fc5-1240cae7d8ab'},False)
        self.run_policy({FLOOR:floor()},False)
        self.run_policy({FLOOR:floor(),SCHEDULER:core(4)},False)
        self.run_policy({FLOOR:floor(),TASKS:core(3)},False)

    def test_stable_persistent_only_in_current_cores(self):
        self.run_policy({FLOOR:floor(),SCHEDULER:core(4,'systemd-persistent-v1'),TASKS:core(3,'systemd-persistent-v1')},True)
        self.run_policy({SCHEDULER:core(2,'systemd-persistent-v1')},False)
        self.run_policy({TASKS:core(1,'systemd-persistent-v1')},False)
        legacy=floor();legacy.update(scheduler_support=4,system_task_support=2,scheduler_schema=3,system_task_schema=2)
        self.run_policy({FLOOR:legacy,SCHEDULER:core(3),TASKS:core(2)},True)
        self.run_policy({FLOOR:legacy,SCHEDULER:core(3,'systemd-persistent-v1'),TASKS:core(2)},False)

    def test_adapter_and_floor_cannot_be_lost(self):
        for path, schema in [(SCHEDULER, 4), (TASKS, 3)]:
            self.run_policy({FLOOR: floor(), SCHEDULER: core(4), TASKS:core(3), path: core(schema, '')}, False)
            self.run_policy({FLOOR: floor(), SCHEDULER: core(4), TASKS:core(3), path: core(schema, 'unknown')}, False)
        self.run_policy({FLOOR: floor(), SCHEDULER: core(2)}, False)


if __name__ == '__main__':
    unittest.main()
