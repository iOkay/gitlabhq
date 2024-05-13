package dependencyproxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab/workhorse/internal/api"
	"gitlab.com/gitlab-org/gitlab/workhorse/internal/badgateway"
	"gitlab.com/gitlab-org/gitlab/workhorse/internal/testhelper"
	"gitlab.com/gitlab-org/gitlab/workhorse/internal/transport"
	"gitlab.com/gitlab-org/gitlab/workhorse/internal/upload"
)

type fakeUploadHandler struct {
	request  *http.Request
	body     []byte
	skipBody bool
	handler  func(w http.ResponseWriter, r *http.Request)
}

const (
	tokenJSON = `{"Token": "token", "Url": "`
	urlJSON   = `/url"}`
)

func (f *fakeUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.request = r

	if !f.skipBody {
		f.body, _ = io.ReadAll(r.Body)
	}

	f.handler(w, r)
}

type errWriter struct{ writes int }

func (w *errWriter) Header() http.Header { return make(http.Header) }
func (w *errWriter) WriteHeader(_ int)   {}

// First call of Write function succeeds while all the subsequent ones fail
func (w *errWriter) Write(p []byte) (int, error) {
	if w.writes > 0 {
		return 0, fmt.Errorf("client error")
	}

	w.writes++

	return len(p), nil
}

type fakePreAuthHandler struct{}

func (f *fakePreAuthHandler) PreAuthorizeHandler(handler api.HandleFunc, _ string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, &api.Response{TempPath: "../../testdata/scratch"})
	})
}

func TestInject(t *testing.T) {
	contentLength := 32768 + 1
	content := strings.Repeat("p", contentLength)

	testCases := []struct {
		desc                string
		responseWriter      http.ResponseWriter
		contentLength       int
		handlerMustBeCalled bool
	}{
		{
			desc:                "the uploading successfully finalized",
			responseWriter:      httptest.NewRecorder(),
			contentLength:       contentLength,
			handlerMustBeCalled: true,
		}, {
			desc:                "a user failed to receive the response",
			responseWriter:      &errWriter{},
			contentLength:       contentLength,
			handlerMustBeCalled: false,
		}, {
			desc:                "the origin resource server returns partial response",
			responseWriter:      httptest.NewRecorder(),
			contentLength:       contentLength + 1,
			handlerMustBeCalled: false,
		},
	}
	testhelper.ConfigureSecret()

	for _, tc := range testCases {
		originResourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(tc.contentLength))
			w.Write([]byte(content))
		}))
		defer originResourceServer.Close()

		// RequestBody expects http.Handler as its second param, we can create a stub function and verify that
		// it's only called for successful requests
		handlerIsCalled := false
		handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerIsCalled = true })

		bodyUploader := upload.RequestBody(&fakePreAuthHandler{}, handlerFunc, &upload.DefaultPreparer{})

		injector := NewInjector()
		injector.SetUploadHandler(bodyUploader)

		r := httptest.NewRequest("GET", "/target", nil)
		sendData := base64.StdEncoding.EncodeToString([]byte(tokenJSON + originResourceServer.URL + urlJSON))

		injector.Inject(tc.responseWriter, r, sendData)

		require.Equal(t, tc.handlerMustBeCalled, handlerIsCalled, "a partial file must not be saved")
	}
}

func TestSuccessfullRequest(t *testing.T) {
	content := []byte("result")
	contentLength := strconv.Itoa(len(content))
	contentType := "foo"
	dockerContentDigest := "sha256:asdf1234"
	overriddenHeader := "originResourceServer"
	originResourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", contentLength)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Docker-Content-Digest", dockerContentDigest)
		w.Header().Set("Overridden-Header", overriddenHeader)
		w.Write(content)
	}))
	defer originResourceServer.Close()

	uploadHandler := &fakeUploadHandler{
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		},
	}

	injector := NewInjector()
	injector.SetUploadHandler(uploadHandler)

	response := makeRequest(injector, tokenJSON+originResourceServer.URL+urlJSON)

	require.Equal(t, "/target/upload", uploadHandler.request.URL.Path)
	require.Equal(t, int64(6), uploadHandler.request.ContentLength)
	require.Equal(t, contentType, uploadHandler.request.Header.Get("Workhorse-Proxy-Content-Type"))
	require.Equal(t, dockerContentDigest, uploadHandler.request.Header.Get("Docker-Content-Digest"))
	require.Equal(t, overriddenHeader, uploadHandler.request.Header.Get("Overridden-Header"))

	require.Equal(t, content, uploadHandler.body)

	require.Equal(t, 200, response.Code)
	require.Equal(t, string(content), response.Body.String())
	require.Equal(t, contentLength, response.Header().Get("Content-Length"))
	require.Equal(t, dockerContentDigest, response.Header().Get("Docker-Content-Digest"))
}

func TestValidUploadConfiguration(t *testing.T) {
	content := []byte("content")
	contentLength := strconv.Itoa(len(content))
	contentType := "text/plain"
	testHeader := "test-received-url"
	originResourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(testHeader, r.URL.Path)
		w.Header().Set("Content-Length", contentLength)
		w.Header().Set("Content-Type", contentType)
		w.Write(content)
	}))
	defer originResourceServer.Close()

	testCases := []struct {
		desc           string
		uploadConfig   *uploadConfig
		expectedConfig uploadConfig
	}{
		{
			desc: "with the default values",
			expectedConfig: uploadConfig{
				Method: http.MethodPost,
				URL:    "/target/upload",
			},
		}, {
			desc: "with overridden method",
			uploadConfig: &uploadConfig{
				Method: http.MethodPut,
			},
			expectedConfig: uploadConfig{
				Method: http.MethodPut,
				URL:    "/target/upload",
			},
		}, {
			desc: "with overridden url",
			uploadConfig: &uploadConfig{
				URL: "http://test.org/overriden/upload",
			},
			expectedConfig: uploadConfig{
				Method: http.MethodPost,
				URL:    "http://test.org/overriden/upload",
			},
		}, {
			desc: "with overridden headers",
			uploadConfig: &uploadConfig{
				Headers: map[string][]string{"Private-Token": {"123456789"}},
			},
			expectedConfig: uploadConfig{
				Headers: map[string][]string{"Private-Token": {"123456789"}},
				Method:  http.MethodPost,
				URL:     "/target/upload",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			uploadHandler := &fakeUploadHandler{
				handler: func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, tc.expectedConfig.URL, r.URL.String())
					require.Equal(t, tc.expectedConfig.Method, r.Method)

					if tc.expectedConfig.Headers != nil {
						for k, v := range tc.expectedConfig.Headers {
							require.Equal(t, v, r.Header[k])
						}
					}

					w.WriteHeader(200)
				},
			}

			injector := NewInjector()
			injector.SetUploadHandler(uploadHandler)

			sendData := map[string]interface{}{
				"Token": "token",
				"Url":   originResourceServer.URL + `/remote/file`,
			}

			if tc.uploadConfig != nil {
				sendData["UploadConfig"] = tc.uploadConfig
			}

			sendDataJSONString, err := json.Marshal(sendData)
			require.NoError(t, err)

			response := makeRequest(injector, string(sendDataJSONString))

			// checking the response
			require.Equal(t, 200, response.Code)
			require.Equal(t, string(content), response.Body.String())
			// checking remote file request
			require.Equal(t, "/remote/file", response.Header().Get(testHeader))
		})
	}
}

func TestInvalidUploadConfiguration(t *testing.T) {
	baseSendData := map[string]interface{}{
		"Token": "token",
		"Url":   "http://remote.dev/remote/file",
	}
	testCases := []struct {
		desc     string
		sendData map[string]interface{}
	}{
		{
			desc: "with an invalid overridden method",
			sendData: mergeMap(baseSendData, map[string]interface{}{
				"UploadConfig": map[string]string{
					"Method": "TEAPOT",
				},
			}),
		}, {
			desc: "with an invalid url",
			sendData: mergeMap(baseSendData, map[string]interface{}{
				"UploadConfig": map[string]string{
					"Url": "invalid_url",
				},
			}),
		}, {
			desc: "with an invalid headers",
			sendData: mergeMap(baseSendData, map[string]interface{}{
				"UploadConfig": map[string]interface{}{
					"Headers": map[string]string{
						"Private-Token": "not_an_array",
					},
				},
			}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			sendDataJSONString, err := json.Marshal(tc.sendData)
			require.NoError(t, err)

			response := makeRequest(NewInjector(), string(sendDataJSONString))

			require.Equal(t, 500, response.Code)
			require.Equal(t, "Internal Server Error\n", response.Body.String())
		})
	}
}

func TestTimeoutConfiguration(t *testing.T) {
	originResourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
	}))
	defer originResourceServer.Close()

	injector := NewInjector()

	var oldHTTPClient = httpClient
	httpClient = &http.Client{
		Transport: transport.NewRestrictedTransport(transport.WithResponseHeaderTimeout(10 * time.Millisecond)),
	}

	t.Cleanup(func() {
		httpClient = oldHTTPClient
	})

	sendData := map[string]string{
		"Url": originResourceServer.URL + "/file",
	}

	sendDataJSONString, err := json.Marshal(sendData)
	require.NoError(t, err)

	response := makeRequest(injector, string(sendDataJSONString))
	responseResult := response.Result()
	defer responseResult.Body.Close()
	require.Equal(t, http.StatusGatewayTimeout, responseResult.StatusCode)
}

func mergeMap(from map[string]interface{}, into map[string]interface{}) map[string]interface{} {
	for k, v := range from {
		into[k] = v
	}
	return into
}

func TestIncorrectSendData(t *testing.T) {
	response := makeRequest(NewInjector(), "")

	require.Equal(t, 500, response.Code)
	require.Equal(t, "Internal Server Error\n", response.Body.String())
}

func TestIncorrectSendDataUrl(t *testing.T) {
	response := makeRequest(NewInjector(), `{"Token": "token", "Url": "url"}`)

	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Equal(t, "Bad Gateway\n", response.Body.String())
}

func TestFailedOriginServer(t *testing.T) {
	originResourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("Not found"))
	}))

	uploadHandler := &fakeUploadHandler{
		handler: func(w http.ResponseWriter, r *http.Request) {
			require.FailNow(t, "the error response must not be uploaded")
		},
	}

	injector := NewInjector()
	injector.SetUploadHandler(uploadHandler)

	response := makeRequest(injector, tokenJSON+originResourceServer.URL+urlJSON)

	require.Equal(t, 404, response.Code)
	require.Equal(t, "Not found", response.Body.String())
}

// This test simulates a situation where the client closes the connection
// before the upload part of the dependency proxy has time to end
func TestLongUploadRequest(t *testing.T) {
	content := []byte("result")
	contentLength := strconv.Itoa(len(content))

	// the server holding the upstream resource
	originResourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", contentLength)
		w.Write(content)
	}))
	defer originResourceServer.Close()

	// the server receiving the upload request
	// it makes the upload request artifically long with a sleep
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(40 * time.Millisecond)
	}))
	defer uploadServer.Close()

	uploadHandler := &fakeUploadHandler{skipBody: true}
	uploadHandler.handler = func(w http.ResponseWriter, r *http.Request) {
		// we need to get the upstream resource through the badgateway roundtripper.
		// It is responsible to handle the response of the client closes the connection
		// abruptly
		rt := badgateway.NewRoundTripper(false, http.DefaultTransport)
		res, err := rt.RoundTrip(r)

		require.NoError(t, err, "RoundTripper should not receive an error")
		require.Equal(t, http.StatusOK, res.StatusCode, "RoundTripper should receive a 200 status code")
		w.WriteHeader(res.StatusCode)
	}

	injector := NewInjector()
	injector.SetUploadHandler(uploadHandler)

	// the client request that simulates a connection closure with a timeout context
	// note that the timeout duration here is shorter than the sleep in the upload server
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", uploadServer.URL+"/upload", nil).WithContext(ctx)
	r.Header.Set("Overridden-Header", "request")

	response := makeCustomRequest(injector, `{"Token": "token", "Url": "`+originResourceServer.URL+`/upstream"}`, r)

	// wait for the slow upload to finish
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, string(content), response.Body.String())
	require.Equal(t, contentLength, response.Header().Get("Content-Length"))
}

func makeRequest(injector *Injector, data string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/target", nil)
	r.Header.Set("Overridden-Header", "request")

	return makeCustomRequest(injector, data, r)
}

func makeCustomRequest(injector *Injector, data string, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	sendData := base64.StdEncoding.EncodeToString([]byte(data))
	injector.Inject(w, r, sendData)

	return w
}
