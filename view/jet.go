package view

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/kataras/iris/v12/context"

	"github.com/CloudyKit/jet/v4"
)

const jetEngineName = "jet"

// JetEngine is the jet template parser's view engine.
type JetEngine struct {
	fs        http.FileSystem
	rootDir   string
	extension string

	loader jet.Loader

	developmentMode bool

	// The Set is the `*jet.Set`, exported to offer any custom capabilities that jet users may want.
	// Available after `Load`.
	Set *jet.Set
	mu  sync.Mutex

	// Note that global vars and functions are set in a single spot on the jet parser.
	// If AddFunc or AddVar called before `Load` then these will be set here to be used via `Load` and clear.
	vars map[string]interface{}

	jetDataContextKey string
}

var (
	_ Engine       = (*JetEngine)(nil)
	_ EngineFuncer = (*JetEngine)(nil)
)

// jet library does not export or give us any option to modify them via Set
// (unless we parse the files by ourselves but this is not a smart choice).
var jetExtensions = [...]string{
	".html.jet",
	".jet.html",
	".jet",
}

// Jet creates and returns a new jet view engine.
// The given "extension" MUST begin with a dot.
//
// Usage:
// Jet("./views", ".jet") or
// Jet(iris.Dir("./views"), ".jet") or
// Jet(AssetFile(), ".jet") for embedded data.
func Jet(fs interface{}, extension string) *JetEngine {
	extOK := false
	for _, ext := range jetExtensions {
		if ext == extension {
			extOK = true
			break
		}
	}

	if !extOK {
		panic(fmt.Sprintf("%s extension is not a valid jet engine extension[%s]", extension, strings.Join(jetExtensions[0:], ", ")))
	}

	s := &JetEngine{
		rootDir:           "/",
		extension:         extension,
		loader:            &jetLoader{fs: getFS(fs)},
		jetDataContextKey: "_jet",
	}

	return s
}

// String returns the name of this view engine, the "jet".
func (s *JetEngine) String() string {
	return jetEngineName
}

// RootDir sets the directory to be used as a starting point
// to load templates from the provided file system.
func (s *JetEngine) RootDir(root string) *JetEngine {
	s.rootDir = filepath.ToSlash(root)
	return s
}

// Ext should return the final file extension which this view engine is responsible to render.
func (s *JetEngine) Ext() string {
	return s.extension
}

// Delims sets the action delimiters to the specified strings, to be used in
// templates. An empty delimiter stands for the
// corresponding default: {{ or }}.
// Should act before `Load` or `iris.Application#RegisterView`.
func (s *JetEngine) Delims(left, right string) *JetEngine {
	s.Set.Delims(left, right)
	return s
}

// JetArguments is a type alias of `jet.Arguments`,
// can be used on `AddFunc$funcBody`.
type JetArguments = jet.Arguments

// AddFunc should adds a global function to the jet template set.
func (s *JetEngine) AddFunc(funcName string, funcBody interface{}) {
	// if something like "urlpath" is registered.
	if generalFunc, ok := funcBody.(func(string, ...interface{}) string); ok {
		// jet, unlike others does not accept a func(string, ...interface{}) string,
		// instead it wants:
		// func(JetArguments) reflect.Value.

		s.AddVar(funcName, jet.Func(func(args JetArguments) reflect.Value {
			n := args.NumOfArguments()
			if n == 0 { // no input, don't execute the function, panic instead.
				panic(funcName + " expects one or more input arguments")
			}

			firstInput := args.Get(0).String()

			if n == 1 { // if only the first argument is given.
				return reflect.ValueOf(generalFunc(firstInput))
			}

			// if has variadic.

			variadicN := n - 1
			variadicInputs := make([]interface{}, variadicN) // except the first one.

			for i := 0; i < variadicN; i++ {
				variadicInputs[i] = args.Get(i + 1).Interface()
			}

			return reflect.ValueOf(generalFunc(firstInput, variadicInputs...))
		}))

		return
	}

	if jetFunc, ok := funcBody.(jet.Func); !ok {
		alternativeJetFunc, ok := funcBody.(func(JetArguments) reflect.Value)
		if !ok {
			panic(fmt.Sprintf("JetEngine.AddFunc: funcBody argument is not a type of func(JetArguments) reflect.Value. Got %T instead", funcBody))
		}

		s.AddVar(funcName, jet.Func(alternativeJetFunc))
	} else {
		s.AddVar(funcName, jetFunc)
	}
}

// AddVar adds a global variable to the jet template set.
func (s *JetEngine) AddVar(key string, value interface{}) {
	if s.Set != nil {
		s.Set.AddGlobal(key, value)
	} else {
		if s.vars == nil {
			s.vars = make(map[string]interface{})
		}
		s.vars[key] = value
	}
}

// Reload if setted to true the templates are reloading on each render,
// use it when you're in development and you're boring of restarting
// the whole app when you edit a template file.
//
// Note that if `true` is passed then only one `View -> ExecuteWriter` will be render each time,
// not safe concurrent access across clients, use it only on development state.
func (s *JetEngine) Reload(developmentMode bool) *JetEngine {
	s.developmentMode = developmentMode
	if s.Set != nil {
		s.Set.SetDevelopmentMode(developmentMode)
	}
	return s
}

// SetLoader can be used when the caller wants to use something like
// multi.Loader or httpfs.Loader.
func (s *JetEngine) SetLoader(loader jet.Loader) *JetEngine {
	s.loader = loader
	return s
}

type jetLoader struct {
	fs http.FileSystem
}

var _ jet.Loader = (*jetLoader)(nil)

// Open opens a file from file system.
func (l *jetLoader) Open(name string) (io.ReadCloser, error) {
	return l.fs.Open(name)
}

// Exists checks if the template name exists by walking the list of template paths
// returns string with the full path of the template and bool true if the template file was found
func (l *jetLoader) Exists(name string) (string, bool) {
	if _, err := l.fs.Open(name); err == nil {
		return name, true
	}

	return "", false
}

// Load should load the templates from a physical system directory or by an embedded one (assets/go-bindata).
func (s *JetEngine) Load() error {
	s.initSet()

	// Note that, unlike the rest of template engines implementations,
	// we don't call the Set.GetTemplate to parse the templates,
	// we let it to the jet template parser itself which does that at serve-time and caches each template by itself.

	return nil
}

// ParseTemplate accepts a name and contnets to parse and cache a template.
// This parser does not support funcs per template. Use the `AddFunc` instead.
func (s *JetEngine) ParseTemplate(name string, contents string) error {
	s.initSet()

	_, err := s.Set.LoadTemplate(name, contents)
	return err
}

func (s *JetEngine) initSet() {
	s.mu.Lock()
	if s.Set == nil {
		s.Set = jet.NewHTMLSetLoader(s.loader)
		s.Set.SetDevelopmentMode(s.developmentMode)

		if s.vars != nil {
			for key, value := range s.vars {
				s.Set.AddGlobal(key, value)
			}
		}
	}
	s.mu.Unlock()
}

type (
	// JetRuntimeVars is a type alias for `jet.VarMap`.
	// Can be used at `AddJetRuntimeVars/JetEngine.AddRuntimeVars`
	// to set a runtime variable ${name} to the executing template.
	JetRuntimeVars = jet.VarMap

	// JetRuntime is a type alias of `jet.Runtime`,
	// can be used on RuntimeVariable input function.
	JetRuntime = jet.Runtime
)

// JetRuntimeVarsContextKey is the Iris Context key to keep any custom jet runtime variables.
// See `AddJetRuntimeVars` package-level function and `JetEngine.AddRuntimeVars` method.
const JetRuntimeVarsContextKey = "iris.jetvarmap"

// AddJetRuntimeVars sets or inserts runtime jet variables through the Iris Context.
// This gives the ability to add runtime variables from different handlers in the request chain,
// something that the jet template parser does not offer at all.
//
// Usage: view.AddJetRuntimeVars(ctx, view.JetRuntimeVars{...}).
// See `JetEngine.AddRuntimeVars` too.
func AddJetRuntimeVars(ctx *context.Context, jetVarMap JetRuntimeVars) {
	if v := ctx.Values().Get(JetRuntimeVarsContextKey); v != nil {
		if vars, ok := v.(JetRuntimeVars); ok {
			for key, value := range jetVarMap {
				vars[key] = value
			}
			return
		}
	}

	ctx.Values().Set(JetRuntimeVarsContextKey, jetVarMap)
}

// AddRuntimeVars sets or inserts runtime jet variables through the Iris Context.
// This gives the ability to add runtime variables from different handlers in the request chain,
// something that the jet template parser does not offer at all.
//
// Usage: view.AddJetRuntimeVars(ctx, view.JetRuntimeVars{...}).
// See `view.AddJetRuntimeVars` if package-level access is more meanful to the code flow.
func (s *JetEngine) AddRuntimeVars(ctx *context.Context, vars JetRuntimeVars) {
	AddJetRuntimeVars(ctx, vars)
}

// ExecuteWriter should execute a template by its filename with an optional layout and bindingData.
func (s *JetEngine) ExecuteWriter(w io.Writer, filename string, layout string, bindingData interface{}) error {
	tmpl, err := s.Set.GetTemplate(filename)
	if err != nil {
		return err
	}

	var vars JetRuntimeVars

	if ctx, ok := w.(*context.Context); ok {
		runtimeVars := ctx.Values().Get(JetRuntimeVarsContextKey)
		if runtimeVars != nil {
			if jetVars, ok := runtimeVars.(JetRuntimeVars); ok {
				vars = jetVars
			}
		}

		if v := ctx.Values().Get(s.jetDataContextKey); v != nil {
			if bindingData == nil {
				// if bindingData is nil, try to fill them by context key (a middleware can set data).
				bindingData = v
			} else if m, ok := bindingData.(context.Map); ok {
				// else if bindingData are passed to App/Context.View
				// and it's map try to fill with the new values passed from a middleware.
				if mv, ok := v.(context.Map); ok {
					for key, value := range mv {
						m[key] = value
					}
				}
			}
		}
	}

	if bindingData == nil {
		return tmpl.Execute(w, vars, nil)
	}

	if vars == nil {
		vars = make(JetRuntimeVars)
	}

	/* fixed on jet v4.0.0, so no need of this:
	if m, ok := bindingData.(context.Map); ok {
		var jetData interface{}
		for k, v := range m {
			if k == s.jetDataContextKey {
				jetData = v
				continue
			}

			if value, ok := v.(reflect.Value); ok {
				vars[k] = value
			} else {
				vars[k] = reflect.ValueOf(v)
			}
		}

		if jetData != nil {
			bindingData = jetData
		}
	}*/

	return tmpl.Execute(w, vars, bindingData)
}
