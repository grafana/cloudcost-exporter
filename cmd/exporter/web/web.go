package web

import (
	"fmt"
	"net/http"
)

const homepageTemplate = `<!doctype html>
<html>
  <head><title>Cloudcost Exporter</title></head>
  <body>
    <h1>Cloudcost Exporter</h1>
    <p><a href=%q>Metrics</a></p>
  </body>
</html>`

func HomePageHandler(metricsPath string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			fmt.Fprintf(w, homepageTemplate, metricsPath)
		} else {
			http.NotFound(w, r)
		}
	}
}
