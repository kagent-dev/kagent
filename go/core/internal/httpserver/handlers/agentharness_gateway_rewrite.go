package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// shouldRewriteGatewayQuotedPath returns true for root-absolute app paths we proxy,
// not for short tokens like "/g" (RegExp flags) or other non-asset paths.
func shouldRewriteGatewayQuotedPath(path string) bool {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return false
	}
	switch {
	case strings.HasPrefix(path, "/assets"):
		return true
	case strings.HasPrefix(path, "/manifest"):
		return true
	case strings.HasPrefix(path, "/favicon"):
		return true
	case path == "/vite.svg":
		return true
	default:
		return false
	}
}

// rewriteGatewayRootPaths prefixes root-absolute URLs in HTML/JS/CSS so assets load under
// /api/agentharnesses/{ns}/{name}/gateway/ (OpenClaw CSP blocks <base>; base-uri 'none').
func rewriteGatewayRootPaths(body []byte, prefix string) []byte {
	if len(body) == 0 || prefix == "" {
		return body
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var out bytes.Buffer
	out.Grow(len(body) + len(prefix)*4)
	s := string(body)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c == '"' || c == '\'') && i+1 < len(s) && s[i+1] == '/' {
			if i+2 < len(s) && s[i+2] == '/' {
				out.WriteByte(c)
				continue
			}
			quote := c
			j := i + 1
			for j < len(s) && s[j] != quote {
				j++
			}
			path := s[i+1 : j]
			out.WriteByte(quote)
			if shouldRewriteGatewayQuotedPath(path) {
				out.WriteString(prefix)
				out.WriteString(strings.TrimPrefix(path, "/"))
			} else {
				out.WriteString(path)
			}
			if j < len(s) {
				out.WriteByte(quote)
			}
			i = j
			continue
		}
		if i+4 < len(s) && strings.EqualFold(s[i:i+4], "url(") {
			j := i + 4
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) && (s[j] == '"' || s[j] == '\'') {
				quote := s[j]
				if j+1 < len(s) && s[j+1] == '/' && !(j+2 < len(s) && s[j+2] == '/') {
					k := j + 1
					for k < len(s) && s[k] != quote {
						k++
					}
					path := s[j+1 : k]
					out.WriteString(s[i : j+1])
					if shouldRewriteGatewayQuotedPath(path) {
						out.WriteString(prefix)
						out.WriteString(strings.TrimPrefix(path, "/"))
					} else {
						out.WriteString(path)
					}
					if k < len(s) {
						out.WriteByte(quote)
					}
					i = k
					continue
				}
			} else if j < len(s) && s[j] == '/' && !(j+1 < len(s) && s[j+1] == '/') {
				k := j + 1
				for k < len(s) && s[k] != ')' && s[k] != ' ' && s[k] != '\t' && s[k] != '"' && s[k] != '\'' {
					k++
				}
				path := s[j:k]
				out.WriteString(s[i:j])
				if shouldRewriteGatewayQuotedPath(path) {
					out.WriteString(prefix)
					out.WriteString(strings.TrimPrefix(path, "/"))
				} else {
					out.WriteString(path)
				}
				i = k - 1
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}

func stripGatewayBaseTag(body []byte) []byte {
	lower := bytes.ToLower(body)
	for {
		idx := bytes.Index(lower, []byte("<base"))
		if idx < 0 {
			break
		}
		end := bytes.Index(lower[idx:], []byte(">"))
		if end < 0 {
			break
		}
		endIdx := idx + end + 1
		body = append(append(body[:idx], body[endIdx:]...))
		lower = bytes.ToLower(body)
	}
	return body
}

func stripGatewayCSP(body []byte) []byte {
	lower := bytes.ToLower(body)
	for _, tag := range []string{
		`<meta http-equiv="content-security-policy"`,
		`<meta http-equiv='content-security-policy'`,
	} {
		for {
			idx := bytes.Index(lower, []byte(tag))
			if idx < 0 {
				break
			}
			end := bytes.Index(lower[idx:], []byte(">"))
			if end < 0 {
				break
			}
			endIdx := idx + end + 1
			body = append(append(body[:idx], body[endIdx:]...))
			lower = bytes.ToLower(body)
		}
	}
	return body
}

func rewriteGatewayBody(body []byte, contentType, prefix string) []byte {
	body = stripGatewayCSP(body)
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") {
		body = stripGatewayBaseTag(body)
	}
	if shouldRewriteGatewayBody(contentType) {
		body = rewriteGatewayRootPaths(body, prefix)
		return rewriteGatewayWebSocketPaths(body, prefix)
	}
	return body
}

// injectGatewayClientShim patches WebSocket URLs (trailing slash + ?token= for OpenClaw Control UI).
func injectGatewayClientShim(body []byte, gatewayToken string) []byte {
	tokenJSON, _ := json.Marshal(gatewayToken)
	shim := fmt.Sprintf(`<script>(function(){var T=%s;var O=WebSocket;`+
		`function fix(u){if(typeof u!=="string")return u;`+
		`if((u.indexOf("ws:")===0||u.indexOf("wss:")===0)&&/\/gateway$/.test(u))u=u+"/";`+
		`if(T&&u.indexOf("token=")<0)u+=(u.indexOf("?")>=0?"&":"?")+"token="+encodeURIComponent(T);`+
		`return u;}`+
		`WebSocket=function(u,p){return new O(fix(u),p)};`+
		`WebSocket.prototype=O.prototype;WebSocket.CONNECTING=O.CONNECTING;`+
		`WebSocket.OPEN=O.OPEN;WebSocket.CLOSING=O.CLOSING;WebSocket.CLOSED=O.CLOSED;`+
		`})();</script>`, tokenJSON)
	lower := bytes.ToLower(body)
	for _, tag := range []string{"</head>", "</HEAD>"} {
		if idx := bytes.Index(lower, []byte(strings.ToLower(tag))); idx >= 0 {
			out := make([]byte, 0, len(body)+len(shim))
			out = append(out, body[:idx]...)
			out = append(out, shim...)
			out = append(out, body[idx:]...)
			return out
		}
	}
	return append(bytes.Clone(body), shim...)
}

// rewriteGatewayWebSocketPaths ensures bundled/runtime WS URLs use .../gateway/ (trailing slash).
// Only rewrites occurrences not already followed by '/' (avoids breaking .../gateway/assets/...).
func rewriteGatewayWebSocketPaths(body []byte, prefix string) []byte {
	gatewayWithSlash := strings.TrimSuffix(prefix, "/") + "/"
	gatewayNoSlash := strings.TrimSuffix(gatewayWithSlash, "/")
	if gatewayNoSlash == "" || gatewayNoSlash == gatewayWithSlash {
		return body
	}
	needle := []byte(gatewayNoSlash)
	var out bytes.Buffer
	out.Grow(len(body) + 16)
	for i := 0; i < len(body); {
		idx := bytes.Index(body[i:], needle)
		if idx < 0 {
			out.Write(body[i:])
			break
		}
		idx += i
		out.Write(body[i:idx])
		end := idx + len(needle)
		if end < len(body) && body[end] == '/' {
			out.Write(needle)
		} else {
			out.Write([]byte(gatewayWithSlash))
		}
		i = end
	}
	return out.Bytes()
}

func shouldRewriteGatewayBody(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "text/css") ||
		strings.Contains(ct, "application/json")
}
