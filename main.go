package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	// Version can be auto-set at build time using an ldflag
	//   go build -ldflags "-X main.Version `git describe --tags --always`"
	Version string

	// DepsFile specifies the Godeps file used by gdm
	DepsFile string = "Godeps"

	// Parallel specifies whether to 'restore' in parallel
	// This is primarily for debug/logging purposes
	Parallel bool = true
)

const usage = `Go Dependency Manager (gdm), a lightweight tool for managing Go dependencies.

Usage:

    gdm <command> [-f GODEPS_FILE] [-v]

The commands are:

    restore   Check out revisions defined in Godeps file to $GOPATH.
    save      Saves currently checked-out dependencies from $GOPATH to Godeps file.
    brew      Outputs homebrew go_resource entries to stdout.
    version   Prints the version.
`

func main() {
	flag.Usage = usageExit
	flag.Parse()
	args := flag.Args()
	var verbose bool
	if len(args) < 1 {
		usageExit()
	} else if len(args) > 1 {
		set := flag.NewFlagSet("", flag.ExitOnError)
		set.StringVar(&DepsFile, "f", "Godeps", "Specify the name/location of Godeps file")
		set.BoolVar(&verbose, "v", false, "Verbose mode")
		set.BoolVar(&Parallel, "parallel", true, "Execute gdm restore in parallel")
		set.Parse(os.Args[2:])
	}

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	gopath, err := getGoPath(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	switch args[0] {
	case "save", "bootstrap":
		splash(wd, gopath)
		save(wd, gopath, verbose)
	case "restore", "get", "sync", "checkout":
		splash(wd, gopath)
		restore(wd, gopath, verbose)
	case "brew", "homebrew":
		splash(wd, gopath)
		homebrew(wd, gopath, verbose)
	case "version":
		fmt.Printf("gdm - version %s\n", Version)
	default:
		usageExit()
	}
}

func splash(wd, gopath string) {
	fmt.Println("======= Go Dependency Manager =======")
	fmt.Println("= working dir:", wd)
	fmt.Println("= GOPATH:     ", gopath)
	fmt.Println("=====================================")
}

func usageExit() {
	fmt.Println(usage)
	os.Exit(0)
}

// getGoPath returns a single GOPATH. If there are multiple defined in the users
// $GOPATH env variable, then getGoPath validates that the working directory is
// part of one of the GOPATHs, and uses the first one it finds that does.
func getGoPath(wd string) (string, error) {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		return "", fmt.Errorf("GOPATH must be set to use gdm")
	}

	// Split out multiple GOPATHs if necessary
	if strings.Contains(gopath, string(os.PathListSeparator)) {
		paths := strings.Split(gopath, string(os.PathListSeparator))
		for _, path := range paths {
			if strings.Contains(wd, path) {
				gopath = path
				break
			}
		}
	}

	if !strings.Contains(wd, gopath) {
		return "", fmt.Errorf("gdm can only be executed within a directory in"+
			" the GOPATH, wd: %s, gopath: %s", wd, gopath)
	}
	return gopath, nil
}

func homebrew(wd, gopath string, verbose bool) {
	imports, err := ImportsFromPath(wd, gopath, verbose)
	if err != nil {
		fmt.Printf("Fatal error: %s", err)
		os.Exit(1)
	}

	fmt.Println()
	for _, i := range imports {
		fmt.Printf("  go_resource \"%s\" do\n", i.ImportPath)
		fmt.Printf("    url \"%s.%s\",\n", i.Repo.Repo, i.Repo.VCS.Cmd)
		fmt.Printf("    :revision => \"%s\"\n", i.Rev)
		fmt.Printf("  end\n")
		fmt.Println()
	}
}

func save(wd, gopath string, verbose bool) {
	imports, err := ImportsFromPath(wd, gopath, verbose)
	if err != nil {
		fmt.Printf("Fatal error: %s", err)
		os.Exit(1)
	}

	f, err := os.Create(filepath.Join(wd, DepsFile))
	if err != nil {
		fmt.Printf("Fatal error: %s", err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, i := range imports {
		fmt.Printf("> Saving Import [%s] Revision [%s]\n", i.ImportPath, i.Rev)
		_, err = w.WriteString(fmt.Sprintf("%s %s\n", i.ImportPath, i.Rev))
		if err != nil {
			fmt.Printf("Fatal error: %s", err)
			os.Exit(1)
		}
	}
	w.Flush()
}

func restore(wd, gopath string, verbose bool) {
	imports := ImportsFromFile(filepath.Join(wd, DepsFile))
	if Parallel {
		restoreParallel(imports, gopath, verbose)
	} else {
		restoreSerial(imports, gopath, verbose)
	}
}

func restoreParallel(imports []*Import, gopath string, verbose bool) {
	var wg sync.WaitGroup
	wg.Add(len(imports))
	errC := make(chan error, len(imports))
	for _, i := range imports {
		i.Verbose = verbose
		go func(I *Import) {
			defer wg.Done()
			err := I.RestoreImport(gopath)
			if err != nil {
				errC <- err
			}
		}(i)
		// arbitrary sleep to avoid overloading a single clone endpoint
		time.Sleep(time.Millisecond * 70)
	}
	wg.Wait()
	close(errC)
	if len(errC) > 0 {
		fmt.Println()
		fmt.Println("ERROR restoring some imports:")
		for err := range errC {
			fmt.Printf("-   %s", err)
		}
		os.Exit(1)
	}
}

func restoreSerial(imports []*Import, gopath string, verbose bool) {
	for _, i := range imports {
		i.Verbose = verbose
		i.RestoreImport(gopath)
	}
}
