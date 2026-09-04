package server

import "sync"

// apt and managed runtime paths are host-wide resources. Keep Node.js and
// Python install/remove operations from competing across separate requests.
var runtimePackageMutationMu sync.Mutex

// App ports are shared by Node.js, Python, and optional PHP application
// servers. Serialize the final availability check with the database write.
var appPortMutationMu sync.Mutex
