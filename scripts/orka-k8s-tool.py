"""Bounded, read-only Kubernetes listing endpoint for the Orka demo."""

import json
import os
import re
import ssl
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

RESOURCES = {
    **{name: "/api/v1" for name in (
        "pods", "services", "namespaces", "nodes", "configmaps", "persistentvolumeclaims"
    )},
    **{name: "/apis/apps/v1" for name in (
        "deployments", "statefulsets", "daemonsets", "replicasets"
    )},
    **{name: "/apis/batch/v1" for name in ("jobs", "cronjobs")},
}
SA = "/var/run/secrets/kubernetes.io/serviceaccount"


def resource_path(args):
    if not isinstance(args, dict) or set(args) - {"resource", "namespace", "phase"}:
        raise ValueError("Expected resource and optional namespace")
    resource, namespace = args.get("resource"), args.get("namespace", "")
    if not isinstance(resource, str) or resource not in RESOURCES:
        raise ValueError("Unsupported resource")
    if not isinstance(namespace, str) or (namespace and not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", namespace)):
        raise ValueError("Invalid namespace")
    if resource in ("nodes", "namespaces") and namespace:
        raise ValueError("This resource is cluster-scoped; omit namespace")
    scope = "/namespaces/" + namespace if namespace else ""
    query = {"limit": "100"}
    phase = args.get("phase", "")
    if not isinstance(phase, str) or phase not in ("", "Pending", "Running", "Succeeded", "Failed", "Unknown"):
        raise ValueError("Invalid pod phase")
    if phase:
        if resource != "pods":
            raise ValueError("Phase applies only to pods")
        query["fieldSelector"] = "status.phase=" + phase
    return RESOURCES[resource] + scope + "/" + resource + "?" + urllib.parse.urlencode(query)


def list_resources(args):
    path = resource_path(args)
    with open(SA + "/token") as token:
        request = urllib.request.Request(
            "https://kubernetes.default.svc" + path,
            headers={"Authorization": "Bearer " + token.read().strip()},
        )
    # Explicit TLS verification and no environment proxy or redirects.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=SA + "/ca.crt")),
    )
    with opener.open(request, timeout=10) as response:
        raw = response.read((4 << 20) + 1)
    if len(raw) > 4 << 20:
        raise ValueError("Resource listing exceeds response limit; select a namespace")
    data = json.loads(raw)
    # Return a compact projection; never return ConfigMap contents or pod env.
    items = []
    for item in data.get("items", []):
        meta, status = item.get("metadata", {}), item.get("status", {})
        row = {"name": meta.get("name"), "namespace": meta.get("namespace", "")}
        for key in ("phase", "readyReplicas", "replicas", "succeeded", "failed"):
            if key in status:
                row[key] = status[key]
        items.append(row)
    return {"resource": args["resource"], "items": items,
            "truncated": bool(data.get("metadata", {}).get("continue"))}


class Handler(BaseHTTPRequestHandler):
    def respond(self, status, body):
        raw = json.dumps(body, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        self.respond(200 if self.path == "/healthz" else 404, {"ok": self.path == "/healthz"})

    def do_POST(self):
        if self.path != "/resources":
            self.respond(404, {"error": "Not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 4096:
                raise ValueError("Expected a JSON body of at most 4096 bytes")
            args = json.loads(self.rfile.read(length))
            self.respond(200, list_resources(args))
        except (ValueError, TypeError):
            self.respond(400, {"error": "Invalid resource request; use an allowed resource and namespace"})
        except urllib.error.HTTPError as err:
            self.respond(502, {"error": "Kubernetes request refused", "status": err.code})
        except (OSError, urllib.error.URLError):
            self.respond(502, {"error": "Kubernetes API unavailable"})

    def setup(self):
        super().setup()
        self.connection.settimeout(15)


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PORT", "8080"))), Handler).serve_forever()
