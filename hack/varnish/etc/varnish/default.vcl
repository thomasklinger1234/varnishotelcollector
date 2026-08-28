vcl 4.1;

import digest;
import fileserver;
import std;

backend default none;

sub vcl_init {
    new file_backend = fileserver.root("/var/www/html");
}

sub vcl_recv {
    # 1. If traceparent doesn't exist, create the base request context
    if (!req.http.traceparent) {
        set req.http.X-Trace-Seed = client.ip + "_" + now + "_" + std.random(1, 1000000);

        # Base Trace ID (32 hex characters)
        set req.http.X-Base-Trace-ID = digest.hash_md5(req.http.X-Trace-Seed);

        # Initial Parent ID (16 hex characters)
        set req.http.X-Base-Span-ID = regsub(digest.hash_sha256(req.http.X-Trace-Seed + "_recv"), "^(.{16}).*$", "\1");

        # Set the client-facing traceparent header
        set req.http.traceparent = "00-" + req.http.X-Base-Trace-ID + "-" + req.http.X-Base-Span-ID + "-01";

        unset req.http.X-Trace-Seed;
    } else {
        # 2. If it already exists, unpack the Trace ID so we can reuse it in backend fetch
        # Regex extracts the 32-char Trace ID from "00-{TraceID}-{SpanID}-{Flags}"
        set req.http.X-Base-Trace-ID = regsub(req.http.traceparent, "^00-([a-f0-9]{32})-([a-f0-9]{16})-[a-f0-9]{2}$", "\1");

        # 3. Always mint a fresh Span ID for this hop at every esi_level,
        # including 0. The inbound span-id belongs to the CALLER (an upstream
        # Varnish, a gateway, or RUM) and is this transaction's PARENT.
        set req.http.X-Span-Seed = req.http.X-Base-Trace-ID + "_" + now + "_" + std.random(1, 1000000) + "_recv";
        set req.http.X-Span-ID = regsub(digest.hash_sha256(req.http.X-Span-Seed), "^(.{16}).*$", "\1");
        set req.http.traceparent = "00-" + req.http.X-Base-Trace-ID + "-" + req.http.X-Span-ID + "-01";
        unset req.http.X-Span-Seed;
        unset req.http.X-Span-ID;
    }
    std.log("OTEL_Attribute: varnish.custom_attr=custom_value");
}

sub vcl_backend_fetch {
    # Ensure we have a Trace ID from vcl_recv to work with
    if (bereq.http.X-Base-Trace-ID) {

        # 3. Generate a brand new, unique Span ID for this specific backend attempt
        set bereq.http.X-Backend-Seed = bereq.http.X-Base-Trace-ID + "_" + now + "_" + std.random(1, 1000000);
        set bereq.http.X-Backend-Span-ID = regsub(digest.hash_sha256(bereq.http.X-Backend-Seed), "^(.{16}).*$", "\1");

        # 4. Overwrite the traceparent header sent to the backend
        # It keeps the original Trace ID but uses the new backend Span ID
        set bereq.http.traceparent = "00-" + bereq.http.X-Base-Trace-ID + "-" + bereq.http.X-Backend-Span-ID + "-01";

        # Clean up internal tracking headers so they aren't sent to the backend
        unset bereq.http.X-Base-Trace-ID;
        unset bereq.http.X-Backend-Seed;
        unset bereq.http.X-Backend-Span-ID;
    }

    set bereq.backend = file_backend.backend();
}

sub vcl_backend_response {
    set beresp.do_esi = true;
}