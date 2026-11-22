"""Bounded real-PTY/plain presentation checks; no host configuration or daemon."""
import json
import os
from pathlib import Path
import pty
import select
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
SOURCE = (ROOT / 'install.sh').read_text()
PRESENTATION = SOURCE.split('# BEGIN TERMINAL PRESENTATION\n', 1)[1].split('# END TERMINAL PRESENTATION', 1)[0]
BINARY = os.environ.get('FLUXO_TERMINAL_BINARY')


def invoke(args, term='xterm-256color', no_colour=False, tty_stream=None):
    environment = {k: v for k, v in os.environ.items() if k not in ('TERM', 'NO_COLOR')}
    if term is not None:
        environment['TERM'] = term
    if no_colour:
        environment['NO_COLOR'] = ''
    master = slave = None
    if tty_stream:
        master, slave = pty.openpty()
    try:
        result = subprocess.run(args, env=environment,
            stdout=slave if tty_stream == 'stdout' else subprocess.PIPE,
            stderr=slave if tty_stream == 'stderr' else subprocess.PIPE, timeout=10)
        terminal = b''
        if slave is not None:
            os.close(slave)
            slave = None
            while select.select([master], [], [], 0.1)[0]:
                try:
                    block = os.read(master, 65536)
                except OSError:
                    break
                if not block:
                    break
                terminal += block
        return result.returncode, (terminal if tty_stream == 'stdout' else result.stdout), (terminal if tty_stream == 'stderr' else result.stderr)
    finally:
        if slave is not None:
            os.close(slave)
        if master is not None:
            os.close(master)


class InstallerPresentation(unittest.TestCase):
    def test_actual_stream_and_plain_fallbacks_preserve_exit_and_message(self):
        for stream in ('stdout', 'stderr', None):
            for term, no_colour in [('xterm-256color', False), ('dumb', False), (None, False), ('unknown', False), ('xterm', True)]:
                with self.subTest(stream=stream, term=term, no_colour=no_colour):
                    code, out, err = invoke(['bash', '-c', 'set -Eeuo pipefail\n' + PRESENTATION +
                        '\nfluxo_message error "ERROR: blocked"\nfluxo_message warning "WARNING: review" >&2\nexit 17'], term, no_colour, stream)
                    self.assertEqual(code, 17)
                    self.assertEqual(b'\x1b[' in out, stream == 'stdout' and term == 'xterm-256color' and not no_colour)
                    self.assertEqual(b'\x1b[' in err, stream == 'stderr' and term == 'xterm-256color' and not no_colour)
                    self.assertIn(b'ERROR: blocked', out)
                    self.assertIn(b'WARNING: review', err)

    def test_success_heading_and_literal_arguments(self):
        code, out, err = invoke(['bash', '-c', PRESENTATION + '\nfluxo_message success "verified %s \\n"\nfluxo_message heading "Next step"'])
        self.assertEqual((code, out, err), (0, b'verified %s \\n\nNext step\n', b''))


@unittest.skipUnless(BINARY, 'set FLUXO_TERMINAL_BINARY to exercise the built CLI')
class BuiltCLI(unittest.TestCase):
    def test_metadata_remains_plain_even_on_a_tty(self):
        for argument in ('--version', '--base-support', '--bridge-contract', '--execution-capabilities', '--installer-tool-versions'):
            with self.subTest(argument=argument):
                code, out, err = invoke([BINARY, argument], tty_stream='stdout')
                self.assertEqual(code, 0, err)
                self.assertNotIn(b'\x1b[', out + err)
                if argument in ('--bridge-contract', '--execution-capabilities'):
                    json.loads(out)

    def test_human_error_colours_stderr_only_and_preserves_exit(self):
        for term, no_colour, stream in [('xterm', False, 'stderr'), ('xterm', False, 'stdout'), ('dumb', False, 'stderr'), (None, False, 'stderr'), ('unknown', False, 'stderr'), ('xterm', True, 'stderr')]:
            with self.subTest(term=term, no_colour=no_colour, stream=stream):
                code, out, err = invoke([BINARY, 'node-toolchain', 'invalid'], term, no_colour, stream)
                self.assertEqual(code, 2)
                self.assertFalse(out)
                self.assertEqual(b'\x1b[' in err, stream == 'stderr' and term == 'xterm' and not no_colour)
                self.assertIn(b'Usage:', err)


if __name__ == '__main__':
    unittest.main()
