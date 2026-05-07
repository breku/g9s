package ui

import "os"

// OriginalStderr holds the process's original os.Stderr captured before
// cmd.root redirects stderr to the g9s log file. Handlers that suspend
// tview to run an interactive child process (e.g. $EDITOR, gcloud compute
// ssh) should wire the child's Stderr to this value so prompts and host-key
// confirmations reach the real terminal instead of the log file.
//
// Nil if cmd.root never set it (e.g. tests). Callers must fall back to
// os.Stderr in that case.
var OriginalStderr *os.File
