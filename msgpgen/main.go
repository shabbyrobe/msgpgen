package main

import (
	"flag"
	"log"
	"os"

	"github.com/pkg/errors"

	"github.com/shabbyrobe/msgpgen"
	"github.com/shabbyrobe/msgpgen/msgpcmd"
	"github.com/shabbyrobe/structer"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config := msgpgen.NewConfig()
	loader := msgpcmd.LoaderConfig{}

	flags := flag.NewFlagSet("msgpgen", flag.ContinueOnError)
	if err := msgpcmd.ConfigFlags(flags, &config); err != nil {
		return err
	}
	if err := msgpcmd.LoaderFlags(flags, &loader); err != nil {
		return err
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if err := gen(loader, config, flags.Args()); err != nil {
		return err
	}
	return nil
}

func gen(loader msgpcmd.LoaderConfig, config msgpgen.Config, args []string) error {
	tpset := structer.NewTypePackageSet()
	dctvCache := msgpgen.NewDirectivesCache(tpset)

	imports, err := msgpcmd.GoList(loader.Imports)
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

	types, err = msgpcmd.FindIfaces(tpset, ifaceNames...)
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
