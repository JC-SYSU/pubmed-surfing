package main

import (
	"fmt"
	"os"

	"github.com/JC-SYSU/pubmed-surfing/internal/runtimectl"
)

func fail(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: pubmed-surfingctl <release|install|verify|activate|run-current>"))
	}
	switch os.Args[1] {
	case "release":
		allow := len(os.Args) > 2 && os.Args[2] == "--allow-dirty"
		paths, e := runtimectl.Release(allow)
		if e != nil {
			fail(e)
		}
		for _, p := range paths {
			fmt.Println(p)
		}
	case "install":
		if len(os.Args) != 3 {
			fail(fmt.Errorf("usage: pubmed-surfingctl install <artifact>"))
		}
		if e := runtimectl.Install(os.Args[2]); e != nil {
			fail(e)
		}
	case "verify":
		path := ""
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		if e := runtimectl.Verify(path); e != nil {
			fail(e)
		}
	case "activate":
		if len(os.Args) != 3 {
			fail(fmt.Errorf("usage: pubmed-surfingctl activate <release-id>"))
		}
		if e := runtimectl.Activate(os.Args[2]); e != nil {
			fail(e)
		}
	case "run-current":
		if e := runtimectl.RunCurrent(); e != nil {
			fail(e)
		}
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}
