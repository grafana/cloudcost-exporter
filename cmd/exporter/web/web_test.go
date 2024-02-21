package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLandingPage(t *testing.T) {
	tests := map[string]struct {
		reqMethod        string
		reqPath          string
		expectedResCode  int
		expectedResTexts []string
	}{
		"simple get":   {reqMethod: "GET", reqPath: "/", expectedResCode: 200, expectedResTexts: []string{"<html>", "<head><title>Cloudcost Exporter</title></head>", "href=\"/metrics\"", "</html>"}},
		"get bad path": {reqMethod: "GET", reqPath: "/asdf", expectedResCode: 404, expectedResTexts: []string{"not found"}},
	}

	handler := http.HandlerFunc(HomePageHandler("/metrics"))

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(test.reqMethod, test.reqPath, nil)
			resRecorder := httptest.NewRecorder()

			handler.ServeHTTP(resRecorder, req)
			gotStatus := resRecorder.Code
			resBody := resRecorder.Body.String()

			assert.Equalf(t, test.expectedResCode, gotStatus, "Wrong status code!  Expected: %v, got: %v", test.expectedResCode, gotStatus)
			for _, expected := range test.expectedResTexts {
				assert.Containsf(t, resBody, expected, "Response body does not contain expected text: %v", expected)
			}
		})
	}
}
