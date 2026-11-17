"""Execute the installer reader gate with isolated privileged I/O substitutes."""
import pathlib,subprocess,sys,unittest
installer=(pathlib.Path(sys.argv.pop(1)).resolve() if len(sys.argv)>1 else pathlib.Path(__file__).resolve().parents[3]/'install.sh')
text=installer.read_text()
function=text.split('check_scheduler_candidate_compatibility() {',1)[1].split('\nbegin_scheduler_installer_lease()',1)[0]
function='check_scheduler_candidate_compatibility() {'+function
# Model only the filesystem boundary. The tested shell predicate is unchanged.
IO=r'''
sudo() {
    if [ "$1" = test ] && [ "$2" = -e ] && [ "$3" = /var/lib/fluxo/execution-readers.json ]; then
        [ "$HAS_FLOOR" = 1 ]; return
    fi
    if [ "$1" = /usr/bin/python3 ] && [ "$2" = -c ]; then
        printf '%s\n' "$REQUIRED"; return
    fi
    if [ "$1" = /usr/bin/python3 ] && [ "$2" = - ]; then
        cat >/dev/null
        [ "$HAS_BACKEND_PROOF" = 0 ]; return
    fi
    return 97
}
'''
class ReaderGate(unittest.TestCase):
 def gate(self,support,floor,required,proof):
  result=subprocess.run(['bash','--noprofile','--norc','-c',IO+function+'\ncheck_scheduler_candidate_compatibility "$SUPPORT"'],env={'PATH':'/usr/bin:/bin','HAS_FLOOR':str(int(floor)),'REQUIRED':str(required),'HAS_BACKEND_PROOF':str(int(proof)),'SUPPORT':str(support)},capture_output=True,timeout=5)
  return result.returncode
 def test_current_reader_with_retained_backend_evidence(self):
  self.assertEqual(self.gate(6,True,6,True),0)
 def test_reader_downgrade_and_unknown_floor_refused(self):
  self.assertNotEqual(self.gate(5,True,6,True),0)
  self.assertNotEqual(self.gate(8,True,6,True),0)
  self.assertNotEqual(self.gate(6,True,7,True),0)
  self.assertEqual(self.gate(7,True,7,True),0)
 def test_legacy_reader_still_refuses_backend_evidence(self):
  self.assertNotEqual(self.gate(2,False,2,True),0)
  self.assertEqual(self.gate(2,False,2,False),0)
if __name__=='__main__':unittest.main()
