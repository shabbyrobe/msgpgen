package main

import (
	"bufio"
	"bytes"
	"flag"
	"go/types"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
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
	Mode      string
	Interface string
	File      string
	Col       int
	Imports   stringList
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
	flags.StringVar(&loader.Mode, "mode", "iface", "Use this mode to search for types. Modes: iface, tsv")
	flags.StringVar(&loader.Interface, "iface", "", "Search for types that implement this interface for generation. Required in iface mode.")
	flags.StringVar(&loader.File, "file", "", "Input file containing types to generate. Required in tsv mode. Pass '-' for stdin.")
	flags.IntVar(&loader.Col, "col", 1, "If using tsv mode, use this 1-indexed whitespace separated column")
	flags.Var(&loader.Imports, "import", "Import these packages to search for types. Comma separated list. Uses go list.")

	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
	if err := run(loader, config, flags.Args()); err != nil {
		log.Fatal(err)
	}
}

func findIfaces(tpset *structer.TypePackageSet, iface structer.TypeName) ([]structer.TypeName, error) {
	var allNamed []*types.Named
	var ifaceNamed *types.Named
	var ts []structer.TypeName

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
	return ts, nil
}

func findTSV(file string, col int) ([]structer.TypeName, error) {
	var rdr io.Reader
	if file == "-" {
		rdr = os.Stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		rdr = f
		defer f.Close()
	}

	split := regexp.MustCompile(`\s+`)
	scanner := bufio.NewScanner(rdr)
	types := []structer.TypeName{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}
		if strings.HasPrefix(line, "//") {
			continue
		}
		t := split.Split(line, -1)[col-1]
		tn, err := structer.ParseTypeName(t)
		if err != nil {
			return nil, err
		}
		types = append(types, tn)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return types, nil
}

func goList(pkgs stringList) ([]string, error) {
	var l []string
	var args = []string{"list"}
	args = append(args, pkgs...)
	cmd := exec.Command("go", args...)

	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(&out)
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

	for _, imp := range imports {
		// should be safe to ignore import errors - it will raise issues
		// if there are any type resolution errors at all, which we don't
		// necessarily care about - we may have incomplete types that won't
		// be complete until the generator runs!
		_, _ = tpset.Import(imp)
	}

	var types []structer.TypeName
	switch loader.Mode {
	case "iface":
		if len(loader.Interface) == 0 {
			return errors.Errorf("-iface arg required when using mode iface")
		}
		tn, err := structer.ParseTypeName(loader.Interface)
		if err != nil {
			return errors.Wrapf(err, "could not parse iface type name %s", tn)
		}
		types, err = findIfaces(tpset, tn)
		if err != nil {
			return err
		}

	case "tsv":
		types, err = findTSV(loader.File, loader.Col)
		if err != nil {
			return errors.Wrap(err, "could not find TSV")
		}

	default:
		return errors.Errorf("unknown loader mode %s", loader.Mode)
	}

	config.Types = types
	return msgpgen.Generate(tpset, dctvCache, config)
}
