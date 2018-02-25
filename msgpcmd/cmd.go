package msgpcmd

import (
	"bufio"
	"bytes"
	"flag"
	"go/types"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"github.com/shabbyrobe/msgpgen"
	"github.com/shabbyrobe/structer"
)

type StringList []string

func (s *StringList) String() string {
	return strings.Join(*s, ",")
}

func (s *StringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type LoaderConfig struct {
	Interfaces StringList
	State      string
	Imports    StringList
}

func ConfigFlags(fs *flag.FlagSet, config *msgpgen.Config) error {
	fs.BoolVar(&config.GenIO, "io", config.GenIO, "create Encode and Decode methods")
	fs.BoolVar(&config.GenMarshal, "marshal", config.GenMarshal, "create Encode and Decode methods")
	fs.BoolVar(&config.GenTests, "tests", config.GenTests, "create tests and benchmarks")
	fs.BoolVar(&config.GenVersion, "ver", config.GenVersion, "generate version files")
	fs.BoolVar(&config.Unexported, "unexported", config.Unexported, "also process unexported types")
	fs.BoolVar(&config.KeepTemp, "keep", config.KeepTemp, "Keep temp files used by generator")
	fs.StringVar(&config.TempDirName, "tempdir", config.TempDirName, "Name of the temp dir used by the generator.")
	fs.StringVar(&config.FileTemplate, "filetpl", config.FileTemplate, "Template of generated file name")
	fs.StringVar(&config.TestTemplate, "testtpl", config.TestTemplate, "Template of generated test file name")
	return nil
}

func LoaderFlags(fs *flag.FlagSet, loader *LoaderConfig) error {
	fs.StringVar(&loader.State, "state", "", "State file for mapping polymorphic types")
	fs.Var(&loader.Interfaces, "ifaces", "Search for types that implement this interface for generation. Comma separated list.")
	fs.Var(&loader.Imports, "import", "Import these packages to search for types. Comma separated list. Uses go list.")
	return nil
}

func FindIfaces(tpset *structer.TypePackageSet, ifaces ...structer.TypeName) ([]structer.TypeName, error) {
	var allNamed []*types.Named
	var ifaceNamed *types.Named
	var ts []structer.TypeName

	for _, iface := range ifaces {
		for _, pkg := range tpset.TypePackages {
			// "no buildable Go source files" == nil pkg.
			if pkg == nil {
				continue
			}

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

func GoList(pkgs []string) ([]string, error) {
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
