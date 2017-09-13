package msgpgen

import (
	"bytes"
	"math/rand"
	"regexp"
	"strings"

	"github.com/fatih/structtag"
	"github.com/pkg/errors"
	"github.com/shabbyrobe/structer"
	"github.com/tinylib/msgp/gen"
	"github.com/tinylib/msgp/parse"
	"github.com/tinylib/msgp/printer"
	"github.com/ttacon/chalk"
)

// Copy-pasta from msgp as there's no easy way to get it without
// ast-wrangling some more.
var primitives = map[string]gen.Primitive{
	"[]byte":         gen.Bytes,
	"string":         gen.String,
	"float32":        gen.Float32,
	"float64":        gen.Float64,
	"complex64":      gen.Complex64,
	"complex128":     gen.Complex128,
	"uint":           gen.Uint,
	"uint8":          gen.Uint8,
	"uint16":         gen.Uint16,
	"uint32":         gen.Uint32,
	"uint64":         gen.Uint64,
	"byte":           gen.Byte,
	"rune":           gen.Int32,
	"int":            gen.Int,
	"int8":           gen.Int8,
	"int16":          gen.Int16,
	"int32":          gen.Int32,
	"int64":          gen.Int64,
	"bool":           gen.Bool,
	"interface{}":    gen.Intf,
	"time.Time":      gen.Time,
	"msgp.Extension": gen.Ext,
}

var (
	msgpPrefixOK   = chalk.Green.String()
	msgpPrefixWarn = chalk.Yellow.String()
	msgpPrefixErr  = chalk.Red.String()
	msgpPrefixJunk = chalk.ResetColor.String()
)

// ParseTag parses the `msg:"..."` struct tag
func ParseTag(tag string) (t structtag.Tag) {
	// structtag errors are unhelpful and unmatchable, just ignore them.
	tags, err := structtag.Parse(tag)
	if err != nil {
		return
	}
	msgpTag, _ := tags.Get("msg")
	if msgpTag == nil {
		return
	}
	return *msgpTag
}

// runMsgp runs msgp's generator, capturing the output.
// WARNING! This will reseed the global RNG to a deterministic
// value while it is running!
func runMsgp(inputFile, outputFile string, config Config) (stdout, stderr bytes.Buffer, err error) {
	return captureStdio(func() error {
		newSeed := rand.Int63()
		rand.Seed(0)
		defer func() {
			rand.Seed(newSeed)
		}()

		msgpfs, err := parse.File(inputFile, config.Unexported)
		if err != nil {
			return err
		}

		if len(msgpfs.Identities) == 0 {
			return nil
		}

		mode := gen.Size
		if config.GenIO {
			mode |= gen.Decode | gen.Encode
		}
		if config.GenMarshal {
			mode |= gen.Marshal | gen.Unmarshal
		}
		if config.GenTests {
			mode |= gen.Test
		}
		if err := printer.PrintFile(outputFile, msgpfs, mode); err != nil {
			return err
		}
		return nil
	})
}

func checkMsgpOutput(tpset *structer.TypePackageSet, dctvs *Directives, seen map[string]bool, line string) error {
	line = strings.TrimSpace(line)
	for strings.HasPrefix(line, msgpPrefixJunk) {
		line = line[len(msgpPrefixJunk):]
	}
	if len(line) == 0 {
		return nil
	}

	if strings.HasPrefix(line, chalk.Magenta.String()+">>> Wrote and formatted") {
		return nil
	}
	if strings.HasPrefix(line, msgpPrefixOK) {
		return nil
	}
	if strings.HasPrefix(line, msgpPrefixWarn) {
		if isIgnoredUnresolved(tpset, dctvs, seen, line) {
			return nil
		}
		return errors.Errorf("msgp generator warning: %s", line[len(msgpPrefixWarn):])
	}
	if strings.HasPrefix(line, msgpPrefixErr) {
		return errors.Errorf("msgp generator failed: %s", line[len(msgpPrefixErr):])
	}
	return errors.Errorf("unhandled msgp generator output: '%s'", line)
}

var ptnUnresolvedIdentifier = regexp.MustCompile(`(?:unresolved|non-local) identifier:\s*([^ ]+)`)

// TODO: need to do a better job of making sure this is expected. See the
// note in structwalker.go.
var ptnIgnoredField = regexp.MustCompile(`: ignored\.`)

// msgp's generator emits an "unresolved identifier" warning or a "non-local
// identifier" warning even if we specify a type has an explicit "msgp:ignore"
// or "msgp:shim" directive
func isIgnoredUnresolved(tpset *structer.TypePackageSet, dctvs *Directives, seen map[string]bool, line string) bool {
	m := ptnUnresolvedIdentifier.FindStringSubmatch(line)
	if len(m) == 2 {
		unresolved := strings.TrimSpace(m[1])
		if seen[unresolved] {
			return true
		}
		for _, v := range dctvs.ignore {
			if unresolved == v {
				return true
			}
		}
		for _, v := range dctvs.intercepted {
			if unresolved == v {
				return true
			}
		}
		return false
	}

	im := ptnIgnoredField.MatchString(line)
	if im {
		return true
	}

	panic(errors.Errorf("Unreadable 'unresolved' line: '%s'", line))
}
