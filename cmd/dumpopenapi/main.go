// dumpopenapi writes the full OpenAPI 3.1 spec for the Composer API to stdout.
//
// Used at build time to regenerate the TypeScript client bindings (and any
// future SDKs) without standing up Postgres, Docker, etc. All Huma handlers
// are registered with zero-valued deps — Huma only reflects on Input/Output
// types at registration time, so handler methods are never invoked here.
//
// The OpenAPI metadata (info, servers, security schemes, tags) and the
// handler registration list both come from internal/api so the dumped spec
// is byte-identical to what the live server publishes at /openapi.json.
//
// Emits JSON to stdout by default; pass `-yaml` for YAML.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/api"
)

func main() {
	asYAML := flag.Bool("yaml", false, "Emit YAML instead of JSON")
	flag.Parse()

	router := chi.NewMux()
	apiInstance := humachi.New(router, api.HumaConfig(composer.Version))

	// Force-register every Huma handler regardless of nil deps so the dumped
	// spec covers the full API surface, not just the runtime-conditional
	// subset.
	api.RegisterHumaHandlers(apiInstance, api.Deps{}, true /* registerAll */)
	api.DocumentRawRoutes(apiInstance)

	spec := apiInstance.OpenAPI()

	if *asYAML {
		data, err := spec.YAML()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Stdout.Write(data)
		return
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}
