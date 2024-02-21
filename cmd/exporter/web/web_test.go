package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLandingPage(t *testing.T) {
	tests := map[string]struct {
		reqMethod       string
		reqPath         string
		expectedResCode int
		expectedResText string
	}{
		"simple get":   {reqMethod: "GET", reqPath: "/", expectedResCode: 200, expectedResText: "Cloudcost Exporter"},
		"get bad path": {reqMethod: "GET", reqPath: "/asdf", expectedResCode: 404, expectedResText: "not found"},
	}

	handler := http.HandlerFunc(HomePageHandler("/"))

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(test.reqMethod, test.reqPath, nil)
			resRecorder := httptest.NewRecorder()

			handler.ServeHTTP(resRecorder, req)
			gotStatus := resRecorder.Code

			assert.Equalf(t, test.expectedResCode, gotStatus, "Wrong status code!  Expected: %v, got: %v", test.expectedResCode, gotStatus)
			assert.Containsf(t, resRecorder.Body.String(), test.expectedResText, "Response body does not contain expected text: %v", test.expectedResText)
		})
	}
}
