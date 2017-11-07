package msgpgen

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/imports"

	"github.com/pkg/errors"
	"github.com/shabbyrobe/structer"
)

type Config struct {
	Types        []structer.TypeName
	GenTests     bool
	GenIO        bool
	GenMarshal   bool
	Unexported   bool
	TempDirName  string
	FileTemplate string
	TestTemplate string
	KeepTemp     bool

	valid bool
}

func NewConfig() Config {
	return Config{
		GenTests:     true,
		GenMarshal:   true,
		GenIO:        true,
		Unexported:   false,
		KeepTemp:     false,
		TempDirName:  "_msgpgen",
		FileTemplate: "{pkg}_msgp_gen.go",
		TestTemplate: "{pkg}_msgp_gen_test.go",
		valid:        true,
	}
}

func Generate(tpset *structer.TypePackageSet, state *State, dctvCache *DirectivesCache, config Config) (err error) {
	if !config.valid {
		return errors.New("please create config using NewConfig(), not Config{}")
	}
	var (
		typq         = NewTypeQueue(tpset)
		tempDirName  = "_msgpgen"
		fileTemplate = "{pkg}_msgp_gen.go"
		testTemplate = "{pkg}_msgp_gen_test.go"
	)

	for _, t := range config.Types {
		if _, err = tpset.Import(t.PackagePath); err != nil {
			fmt.Println("import failed:", t.PackagePath)
		}

		typ := tpset.Objects[t]
		if typ == nil {
			return errors.Errorf("could not find type %s", t)
		}
		typq.AddObj(t.PackagePath, typ)
	}

	ex := newExtractor(tpset, dctvCache, typq, state)

	if err = ex.extract(); err != nil {
		return err
	}

	// map of temp files to destination
	var files = make(map[string]string)

	var cleanup = &Cleanup{}
	if !config.KeepTemp {
		defer cleanup.Cleanup(&err)
	}

	// need to ensure sorted order when iterating over temp output
	var tempKeys []string
	for opkg := range ex.tempOutput {
		tempKeys = append(tempKeys, opkg)
	}
	sort.Strings(tempKeys)

	for _, opkg := range tempKeys {
		outputParts := ex.tempOutput[opkg]

		lpkg := filepath.Base(opkg)

		// FIXME: panic risk
		pkgPath := tpset.ASTPackages.Packages[opkg].FullPath

		var tempDir = filepath.Join(pkgPath, tempDirName)
		os.Mkdir(tempDir, 0700)
		cleanup.Push(tempDir)

		tfn := filepath.Join(tempDir, lpkg+".go")
		tf, err := os.OpenFile(tfn, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0700)
		if err != nil {
			return err
		}
		cleanup.Push(tfn)

		dctv, err := dctvCache.Ensure(opkg)
		if err != nil {
			return err
		}

		{ // generate temp file of joined definitions
			fmt.Printf("\n======= %s %s\n", opkg, tfn)

			fmt.Fprintf(tf, "// +build ignore\n\n")
			fmt.Fprintf(tf, "package %s\n\n", lpkg)

			for _, d := range dctv.directives {
				dout, err := d.Build(opkg)
				if err != nil {
					return err
				}
				outputParts = append(outputParts, dout)
			}

			// consistent output ordering of temporary file should
			// hopefully yield consistent generated code
			sortOutput(outputParts)
			for _, s := range outputParts {
				fmt.Fprintln(tf, s)
				fmt.Fprintln(tf)
			}
			fmt.Fprintln(tf)
		}

		{ // run msgp's generator
			var stdout, stderr bytes.Buffer

			// unfortunately, msgp's generator writes to stdout and we can't do
			// much about it other than capture and parse it for errors.
			// we really want to be strict about what we ignore and what we consider
			// a real message, but this is brittle if upstream ever changes the text. it's
			// better than having the generator spew reams of useless garbage into
			// the terminal on every run, though.

			tgnb := strings.Replace(fileTemplate, "{pkg}", lpkg, -1)
			tgn := filepath.Join(tempDir, tgnb)
			if config.GenTests {
				ttnb := lpkg + "_msgp_gen_test.go"
				ttnd := strings.Replace(testTemplate, "{pkg}", lpkg, -1)
				ttn := filepath.Join(pkgPath, ttnd)
				cleanup.Push(ttn)
				files[filepath.Join(tempDir, ttnb)] = ttn
			}
			files[filepath.Join(tempDir, tgnb)] = filepath.Join(pkgPath, tgnb)

			stdout, stderr, err = runMsgp(tfn, tgn, config)
			if err != nil {
				return errors.Wrap(err, "msgp run failed")
			}
			if stderr.Len() > 0 {
				return errors.Errorf("msgp stderr contained output: %s", stderr.String())
			}

			// Turn seen full type names (a/b/thing.Foo) into a list of seen
			// importable names (thing.Foo) so that we can match types extracted
			// from msgp's errors.
			seen := make(map[string]bool)
			for k := range typq.seenTypes {
				seen[filepath.Base(k)] = true
			}

			// this would be easier with this issue resolved:
			// https://github.com/tinylib/msgp/issues/183
			scanner := bufio.NewScanner(&stdout)
			for scanner.Scan() {
				if err = checkMsgpOutput(tpset, dctv, seen, scanner.Text()); err != nil {
					return err
				}
			}
			if err = scanner.Err(); err != nil {
				return err
			}

			// append any extra generated stuff to the generated output (interceptions)
			if extra, ok := ex.extraOutput[opkg]; ok {
				f, err := os.OpenFile(tgn, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					panic(err)
				}
				func() {
					defer func() {
						if cerr := f.Close(); cerr != nil {
							panic(cerr)
						}
					}()
					sortOutput(extra)
					for _, e := range extra {
						if _, err = f.WriteString(e); err != nil {
							panic(err)
						}
					}
				}()

				src, err := ioutil.ReadFile(tgn)
				if err != nil {
					return err
				}
				// imports is supposed to be able to load data from a file, but that doesn't
				// seem to work so we have to get the src ourselves.
				p, err := imports.Process(tgn, src, nil)
				if err != nil {
					return err
				}
				if err := ioutil.WriteFile(tgn, p, 0600); err != nil {
					return err
				}
			}
		}
	}

	// move the generated file into place, but only if the contents are different
	for src, dest := range files {
		write := false
		destb, err := ioutil.ReadFile(dest)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if os.IsNotExist(err) {
			write = true
		} else {
			srcb, err := ioutil.ReadFile(src)
			if err != nil {
				return err
			}
			write = !bytes.Compare(srcb, dstb)
		}

		if write {
			if err := os.Rename(src, dest); err != nil {
				return err
			}
		} else {
			fmt.Printf("unmodified: %s\n", dest)
			if err := os.Remove(src); err != nil {
				return err
			}
		}
	}

	return nil
}

func outputPriority(part string) int {
	if strings.HasPrefix(strings.TrimSpace(part), "//msgp:shim ") {
		return 1
	}
	return 2
}

func sortOutput(outputParts []string) {
	sort.Slice(outputParts, func(i, j int) bool {
		ap := outputPriority(outputParts[i])
		bp := outputPriority(outputParts[j])
		return ap < bp || (ap == bp && outputParts[i] < outputParts[j])
	})
}
