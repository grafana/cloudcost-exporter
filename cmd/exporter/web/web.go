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
    <p><a href=%q'>Metrics</a></p>
  </body>
</html>`

func HomePageHandler(path string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			fmt.Fprintf(w, homepageTemplate, path)
		} else {
			http.NotFound(w, r)
		}
	}
}
