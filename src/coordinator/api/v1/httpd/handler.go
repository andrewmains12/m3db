// Copyright (c) 2018 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package httpd

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/pprof"
	"os"

	clusterclient "github.com/m3db/m3cluster/client"
	"github.com/m3db/m3db/src/cmd/services/m3coordinator/config"
	dbconfig "github.com/m3db/m3db/src/cmd/services/m3dbnode/config"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler/database"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler/namespace"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler/openapi"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler/placement"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler/prometheus/native"
	"github.com/m3db/m3db/src/coordinator/api/v1/handler/prometheus/remote"
	"github.com/m3db/m3db/src/coordinator/executor"
	"github.com/m3db/m3db/src/coordinator/storage"
	"github.com/m3db/m3db/src/coordinator/util/logging"

	"github.com/gorilla/mux"
	"github.com/uber-go/tally"
	"go.uber.org/zap"
)

const (
	pprofURL  = "/debug/pprof/profile"
	routesURL = "/routes"
)

var (
	remoteSource = map[string]string{"source": "remote"}
)

// Handler represents an HTTP handler.
type Handler struct {
	Router        *mux.Router
	CLFLogger     *log.Logger
	storage       storage.Storage
	engine        *executor.Engine
	clusterClient clusterclient.Client
	config        config.Configuration
	embeddedDbCfg *dbconfig.DBConfiguration
	scope         tally.Scope
}

// NewHandler returns a new instance of handler with routes.
func NewHandler(
	storage storage.Storage,
	engine *executor.Engine,
	clusterClient clusterclient.Client,
	cfg config.Configuration,
	embeddedDbCfg *dbconfig.DBConfiguration,
	scope tally.Scope,
) (*Handler, error) {
	r := mux.NewRouter()
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}

	defer logger.Sync() // flushes buffer, if any
	h := &Handler{
		CLFLogger:     log.New(os.Stderr, "[httpd] ", 0),
		Router:        r,
		storage:       storage,
		engine:        engine,
		clusterClient: clusterClient,
		config:        cfg,
		embeddedDbCfg: embeddedDbCfg,
		scope:         scope,
	}
	return h, nil
}

// RegisterRoutes registers all http routes.
func (h *Handler) RegisterRoutes() error {
	logged := logging.WithResponseTimeLogging

	h.Router.HandleFunc(openapi.URL, logged(&openapi.DocHandler{}).ServeHTTP).Methods(openapi.HTTPMethod)
	h.Router.PathPrefix(openapi.StaticURLPrefix).Handler(logged(openapi.StaticHandler()))

	h.Router.HandleFunc(remote.PromReadURL, logged(remote.NewPromReadHandler(h.engine, h.scope.Tagged(remoteSource))).ServeHTTP).Methods(remote.PromReadHTTPMethod)
	h.Router.HandleFunc(remote.PromWriteURL, logged(remote.NewPromWriteHandler(h.storage, h.scope.Tagged(remoteSource))).ServeHTTP).Methods(remote.PromWriteHTTPMethod)
	h.Router.HandleFunc(native.PromReadURL, logged(native.NewPromReadHandler(h.engine)).ServeHTTP).Methods(native.PromReadHTTPMethod)
	h.Router.HandleFunc(handler.SearchURL, logged(handler.NewSearchHandler(h.storage)).ServeHTTP).Methods(handler.SearchHTTPMethod)

	if h.clusterClient != nil {
		placement.RegisterRoutes(h.Router, h.clusterClient, h.config)
		namespace.RegisterRoutes(h.Router, h.clusterClient)
		database.RegisterRoutes(h.Router, h.clusterClient, h.config, h.embeddedDbCfg)
	}

	h.registerProfileEndpoints()
	h.registerRoutesEndpoint()

	return nil
}

// Endpoints useful for profiling the service
func (h *Handler) registerProfileEndpoints() {
	h.Router.HandleFunc(pprofURL, pprof.Profile)
}

// Endpoints useful for viewing routes directory
func (h *Handler) registerRoutesEndpoint() {
	h.Router.HandleFunc(routesURL, func(w http.ResponseWriter, r *http.Request) {
		var routes []string
		err := h.Router.Walk(
			func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
				str, err := route.GetPathTemplate()
				if err != nil {
					return err
				}
				routes = append(routes, str)
				return nil
			})
		if err != nil {
			handler.Error(w, err, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(struct {
			Routes []string `json:"routes"`
		}{
			Routes: routes,
		})
	})
}
