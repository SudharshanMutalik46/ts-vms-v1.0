package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWindowsDiscoveryHandler_Security(t *testing.T) {
	h := NewWindowsHandler()
	req := httptest.NewRequest("POST", "/api/v1/windows/discovery:scan", nil)
	rr := httptest.NewRecorder()

	h.WindowsDiscoveryHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"result"`)
}

func TestWindowsDiscoveryHandler_Method(t *testing.T) {
	h := NewWindowsHandler()
	req := httptest.NewRequest("GET", "/api/v1/windows/discovery:scan", nil)
	rr := httptest.NewRecorder()

	h.WindowsDiscoveryHandler(rr, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}
