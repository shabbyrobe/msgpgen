package main

import (
	"bufio"
	"bytes"
	"flag"
	"go/types"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"

	"github.com/shabbyrobe/msgpgen"
	"github.com/shabbyrobe/structer"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type LoaderConfig struct {
	Interfaces stringList
	State      string
	Imports    stringList
}

func main() {
	config := msgpgen.NewConfig()
	flags := flag.NewFlagSet("msgpgen", flag.ContinueOnError)
	flags.BoolVar(&config.GenIO, "io", config.GenIO, "create Encode and Decode methods")
	flags.BoolVar(&config.GenMarshal, "marshal", config.GenMarshal, "create Encode and Decode methods")
	flags.BoolVar(&config.GenTests, "tests", config.GenTests, "create tests and benchmarks")
	flags.BoolVar(&config.Unexported, "unexported", config.Unexported, "also process unexported types")
	flags.BoolVar(&config.KeepTemp, "keep", config.KeepTemp, "Keep temp files used by generator")
	flags.StringVar(&config.TempDirName, "tempdir", config.TempDirName, "Name of the temp dir used by the generator.")
	flags.StringVar(&config.FileTemplate, "filetpl", config.FileTemplate, "Template of generated file name")
	flags.StringVar(&config.TestTemplate, "testtpl", config.TestTemplate, "Template of generated test file name")

	loader := LoaderConfig{}
	flags.StringVar(&loader.State, "state", "", "State file for mapping polymorphic types")
	flags.Var(&loader.Interfaces, "ifaces", "Search for types that implement this interface for generation. Comma separated list.")
	flags.Var(&loader.Imports, "import", "Import these packages to search for types. Comma separated list. Uses go list.")

	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
	if err := run(loader, config, flags.Args()); err != nil {
		log.Fatal(err)
	}
}

func findIfaces(tpset *structer.TypePackageSet, ifaces ...structer.TypeName) ([]structer.TypeName, error) {
	var allNamed []*types.Named
	var ifaceNamed *types.Named
	var ts []structer.TypeName

	for _, iface := range ifaces {
		for _, pkg := range tpset.TypePackages {
			for _, name := range pkg.Scope().Names() {
				if obj, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok {
					nn, ok := obj.Type().(*types.Named)
					if ok {
						if nn.String() == iface.String() && types.IsInterface(obj.Type()) {
							ifaceNamed = nn
						} else {
							if _, ok := obj.Type().Underlying().(*types.Struct); ok {
								allNamed = append(allNamed, nn)
							}
						}
					}
				}
			}
		}

		if ifaceNamed == nil {
			return nil, errors.Errorf("interface %s not found in imported packages", iface)
		}

		for _, T := range allNamed {
			if T == ifaceNamed || types.IsInterface(T) {
				continue
			}
			found := false
			if types.AssignableTo(T, ifaceNamed) {
				// fmt.Printf("%s satisfies %s\n", T, ifaceNamed)
				found = true
			} else if types.AssignableTo(types.NewPointer(T), ifaceNamed) {
				// fmt.Printf("%s satisfies %s\n", types.NewPointer(T), ifaceNamed)
				found = true
			}
			if found {
				tn, err := structer.ParseTypeName(T.String())
				if err != nil {
					return nil, errors.Wrapf(err, "could not parse found type name %s", tn)
				}
				ts = append(ts, tn)
			}
		}
	}
	return ts, nil
}

func goList(pkgs stringList) ([]string, error) {
	var l []string
	var args = []string{"list"}
	args = append(args, pkgs...)
	cmd := exec.Command("go", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		errMsg := stderr.String()
		return nil, errors.Wrap(err, errMsg)
	}
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}
		l = append(l, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return l, nil
}

func run(loader LoaderConfig, config msgpgen.Config, args []string) error {
	tpset := structer.NewTypePackageSet()
	dctvCache := msgpgen.NewDirectivesCache(tpset)

	imports, err := goList(loader.Imports)
	if err != nil {
		return errors.Wrapf(err, "go list failed")
	}

	// b, _ := json.Marshal(state)
	// var out bytes.Buffer
	// json.Indent(&out, b, "", "  ")
	// fmt.Println(out.String())

	for _, imp := range imports {
		// should be safe to ignore import errors - it will raise issues
		// if there are any type resolution errors at all, which we don't
		// necessarily care about - we may have incomplete types that won't
		// be complete until the generator runs!
		_, _ = tpset.Import(imp)
	}

	var state *msgpgen.State
	if loader.State != "" {
		if state, err = msgpgen.LoadStateFromFile(loader.State); err != nil {
			return err
		}
	}

	var types []structer.TypeName
	if len(loader.Interfaces) == 0 {
		return errors.Errorf("-ifaces arg required")
	}

	var ifaceNames []structer.TypeName
	for _, i := range loader.Interfaces {
		tn, err := structer.ParseTypeName(i)
		if err != nil {
			return errors.Wrapf(err, "could not parse iface type name %s", tn)
		}
		ifaceNames = append(ifaceNames, tn)
	}

	types, err = findIfaces(tpset, ifaceNames...)
	if err != nil {
		return err
	}

	config.Types = types
	if err := msgpgen.Generate(tpset, state, dctvCache, config); err != nil {
		return err
	}

	if loader.State != "" {
		if err := state.SaveToFile(loader.State); err != nil {
			return err
		}
	}
	return nil
}
