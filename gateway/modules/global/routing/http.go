package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/spaceuptech/helpers"

	"github.com/spaceuptech/space-cloud/gateway/config"
	"github.com/spaceuptech/space-cloud/gateway/model"
	"github.com/spaceuptech/space-cloud/gateway/modules/auth"
	"github.com/spaceuptech/space-cloud/gateway/utils"
)

type modulesInterface interface {
	Auth(string) (*auth.Module, error)
}

var httpClient = http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// HandleRoutes handles incoming http requests and routes them according to the configured rules.
func (r *Routing) HandleRoutes(modules modulesInterface) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		// Close the body of the request
		defer utils.CloseTheCloser(request.Body)

		// Extract the host and url to select route
		host, url := getHostAndURL(request)

		// Select a route based on host and url
		route, err := r.selectRoute(request.Context(), host, request.Method, url)
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
			return
		}

		token, claims, status, err := r.modifyRequest(request.Context(), modules, route, request)
		if err != nil {
			writer.WriteHeader(status)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
			return
		}

		helpers.Logger.LogDebug(helpers.GetRequestID(request.Context()), fmt.Sprintf("selected route (%v) for request (%s)", route, request.URL.String()), nil)

		// Apply the rewrite url if provided. It is the users responsibility to make sure both url
		// and rewrite url starts with a '/'
		url = rewriteURL(url, route)

		// Proxy the request

		if err := setRequest(request.Context(), request, route, url); err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
			_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed set request for route (%v)", route), err, nil)
			return
		}

		var redisKey string
		if route.IsRouteCacheable && request.Method == http.MethodGet {
			cacheOptionsArray := make([]interface{}, 0)
			for _, key := range route.CacheOptions {
				value, err := utils.LoadValue(key, map[string]interface{}{"args": map[string]interface{}{"auth": claims, "token": token, "url": request.URL.String()}})
				if err != nil {
					_ = helpers.Response.SendErrorResponse(request.Context(), writer, http.StatusBadRequest, err)
					return
				}
				cacheOptionsArray = append(cacheOptionsArray, value)
			}

			key, isCacheHit, result, err := r.caching.GetIngressRoute(request.Context(), route.ID, cacheOptionsArray)
			if err != nil {
				_ = helpers.Response.SendErrorResponse(request.Context(), writer, http.StatusBadRequest, err)
				return
			}
			if isCacheHit {
				for k, v := range result.Headers {
					writer.Header()[k] = v
				}
				writer.WriteHeader(http.StatusOK)
				n, err := io.Copy(writer, ioutil.NopCloser(bytes.NewBuffer(result.Body)))
				if err != nil {
					_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed to copy upstream (%s) response to downstream", request.URL.String()), err, nil)
				}
				helpers.Logger.LogDebug(helpers.GetRequestID(request.Context()), fmt.Sprintf("Successfully copied %d bytes from upstream server (%s)", n, request.URL.String()), nil)
				return
			}
			redisKey = key
		}

		// TODO: Use http2 client if that was the incoming request protocol
		response, err := httpClient.Do(request)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
			_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed to make request for route (%v)", route), err, nil)
			return
		}
		defer utils.CloseTheCloser(response.Body)

		if err := r.modifyResponse(request.Context(), response, route, token, claims); err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
			return
		}

		values := response.Header.Get("cache-control")
		if values != "" && route.IsRouteCacheable && redisKey != "" && request.Method == http.MethodGet {
			var cacheTime string
			for _, value := range strings.Split(values, ",") {
				if value == "no-cache" {
					break
				}
				value = strings.TrimSpace(value)
				if strings.HasPrefix(value, "max-age") {
					cacheTime = strings.Split(value, "=")[1]
					break
				}
				if strings.HasPrefix(value, "s-maxage") {
					cacheTime = strings.Split(value, "=")[1]
					break
				}
			}
			if cacheTime != "" {
				duration, err := strconv.Atoi(cacheTime)
				if err != nil {
					_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed to copy upstream (%s) response to downstream", request.URL.String()), err, nil)
				}
				data, err := ioutil.ReadAll(response.Body)
				if err != nil {
					_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed to copy upstream (%s) response to downstream", request.URL.String()), err, nil)
				}
				if err := r.caching.SetIngressRouteKey(request.Context(), redisKey, &config.ReadCacheOptions{TTL: int64(duration)}, &model.CacheIngressRoute{Headers: response.Header, Body: data}); err != nil {
					_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed to copy upstream (%s) response to downstream", request.URL.String()), err, nil)
				}
				response.Body = ioutil.NopCloser(bytes.NewBuffer(data))
			}
		}

		// Copy headers and status code
		for k, v := range response.Header {
			writer.Header()[k] = v
		}
		writer.WriteHeader(response.StatusCode)

		// Copy the body
		n, err := io.Copy(writer, response.Body)
		if err != nil {
			_ = helpers.Logger.LogError(helpers.GetRequestID(request.Context()), fmt.Sprintf("Failed to copy upstream (%s) response to downstream", request.URL.String()), err, nil)
		}

		helpers.Logger.LogDebug(helpers.GetRequestID(request.Context()), fmt.Sprintf("Successfully copied %d bytes from upstream server (%s)", n, request.URL.String()), nil)
	}
}

func getHostAndURL(request *http.Request) (string, string) {
	return strings.Split(request.Host, ":")[0], request.URL.Path
}

func rewriteURL(url string, route *config.Route) string {
	if route.Source.RewriteURL != "" {
		// First strip away the url provided
		url = strings.TrimPrefix(url, route.Source.URL)

		// Apply the rewrite url at the prefix
		url = route.Source.RewriteURL + url
	}
	return url
}

func setRequest(ctx context.Context, request *http.Request, route *config.Route, url string) error {
	// http: Request.RequestURI can't be set in client requests.
	// http://golang.org/src/pkg/net/http/client.go
	request.RequestURI = ""

	// Change the request with the destination host, port and url
	target, err := route.SelectTarget(ctx, -1) // pass a -ve weight to randomly generate
	if err != nil {
		return err
	}

	request.Host = target.Host
	request.URL.Host = fmt.Sprintf("%s:%d", target.Host, target.Port)
	request.URL.Path = url

	// Set the url scheme to http
	if target.Scheme == "" {
		target.Scheme = "http"
	}
	request.URL.Scheme = target.Scheme
	return nil
}

func prepareHeaders(headers config.Headers, state map[string]interface{}) config.Headers {
	out := make([]config.Header, len(headers))
	for i, header := range headers {
		// First create a new header object
		h := config.Header{Key: header.Key, Value: header.Value, Op: header.Op}

		// Load the string if it exists
		value, err := utils.LoadValue(header.Value, state)
		if err == nil {
			if temp, ok := value.(string); ok {
				h.Value = temp
			} else {
				d, _ := json.Marshal(value)
				h.Value = string(d)
			}
		}

		out[i] = h
	}
	return out
}
