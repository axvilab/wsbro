<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>WSBro Integration Test</title>
    <script src="https://js.pusher.com/8.2/pusher.min.js"></script>
    <style>
        body { font-family: monospace; background: #111; color: #eee; padding: 20px; }
        .section { margin: 20px 0; padding: 15px; border: 1px solid #333; border-radius: 8px; }
        .section h3 { margin-top: 0; color: #10b981; }
        button { background: #10b981; color: #111; border: none; padding: 8px 16px; border-radius: 4px; cursor: pointer; margin: 4px; font-family: monospace; }
        button:hover { background: #059669; }
        button.danger { background: #ef4444; }
        #log { background: #000; padding: 10px; border-radius: 4px; height: 300px; overflow-y: auto; font-size: 12px; }
        .log-info { color: #9ca3af; }
        .log-success { color: #10b981; }
        .log-error { color: #ef4444; }
        .log-event { color: #3b82f6; }
        .log-webhook { color: #f59e0b; }
        #status { padding: 4px 12px; border-radius: 12px; font-size: 12px; display: inline-block; }
        .connected { background: #10b98133; color: #10b981; }
        .disconnected { background: #ef444433; color: #ef4444; }
    </style>
</head>
<body>
    <h1>WSBro Integration Test</h1>
    <div>
        Status: <span id="status" class="disconnected">Disconnected</span>
        Socket ID: <span id="socket-id">-</span>
    </div>

    <div class="section">
        <h3>1. Public Channel</h3>
        <button id="btn-sub-public" onclick="subscribePublic()">Subscribe to public-test</button>
        <button id="btn-send-public" onclick="sendPublic()">Send Event</button>
    </div>

    <div class="section">
        <h3>2. Private Channel</h3>
        <button id="btn-sub-private" onclick="subscribePrivate()">Subscribe to private-user.1</button>
        <button id="btn-send-private" onclick="sendPrivate()">Send Event</button>
        <button id="btn-sub-private-noauth" onclick="subscribePrivateNoAuth()">Subscribe Without Auth (should fail)</button>
    </div>

    <div class="section">
        <h3>3. Presence Channel</h3>
        <button id="btn-sub-presence" onclick="subscribePresence()">Subscribe to presence-chat</button>
        <button id="btn-send-presence" onclick="sendPresence()">Send Event</button>
    </div>

    <div class="section">
        <h3>4. Client Events (Whisper)</h3>
        <button id="btn-whisper" onclick="sendWhisper()">Send client-typing</button>
    </div>

    <div class="section">
        <h3>5. Batch Events</h3>
        <button id="btn-batch" onclick="sendBatch()">Send 3 Batch Events</button>
    </div>

    <div class="section">
        <h3>6. Unsubscribe</h3>
        <button id="btn-unsub" onclick="unsubscribePublic()">Unsubscribe from public-test</button>
        <button id="btn-send-after-unsub" onclick="sendPublicAfterUnsub()">Send Event (should not receive)</button>
    </div>

    <div class="section">
        <h3>7. Multi-App</h3>
        <button id="btn-multi-app" onclick="testMultiApp()">Connect with App 2 Key</button>
        <button id="btn-multi-app-invalid" onclick="testInvalidKey()">Connect with Invalid Key</button>
    </div>

    <div class="section">
        <h3>8. Metrics & Dashboard</h3>
        <button onclick="fetchMetrics()">Fetch Metrics</button>
        <a href="http://localhost:6001/dashboard" target="_blank"><button type="button">Open Dashboard</button></a>
    </div>

    <div class="section">
        <h3>9. Webhooks</h3>
        <button onclick="fetchWebhooks()">Check Webhook Log</button>
        <span id="webhook-info" style="color: #f59e0b; font-size: 12px; margin-left: 10px;">Webhooks are logged server-side (check Laravel log)</span>
    </div>

    <div class="section">
        <h3>10. Reconnect</h3>
        <button id="btn-disconnect" onclick="testDisconnect()">Disconnect</button>
        <button id="btn-reconnect" onclick="testReconnect()">Reconnect</button>
    </div>

    <h3>Event Log</h3>
    <div id="log"></div>

    <script>
        let pusher = null;
        let publicChannel = null;
        let privateChannel = null;
        let presenceChannel = null;
        let pusher2 = null;
        Pusher.logToConsole = true;

        // Use URL param ?wsHost=wsbro for Docker, default to page hostname for browser
        const urlParams = new URLSearchParams(window.location.search);
        const wsHost = urlParams.get('wsHost') || window.location.hostname;

        function log(msg, type = 'info') {
            const el = document.getElementById('log');
            const time = new Date().toLocaleTimeString();
            const cls = 'log-' + type;
            el.innerHTML += `<div class="${cls}">[${time}] ${msg}</div>`;
            el.scrollTop = el.scrollHeight;
        }

        function updateStatus(connected) {
            const el = document.getElementById('status');
            el.textContent = connected ? 'Connected' : 'Disconnected';
            el.className = connected ? 'connected' : 'disconnected';
        }

        // Initialize Pusher
        function initPusher() {
            pusher = new Pusher('{{ env("PUSHER_APP_KEY") }}', {
                wsHost: wsHost,
                wsPort: {{ env("PUSHER_PORT", 6001) }},
                forceTLS: false,
                enabledTransports: ['ws'],
                cluster: '',
                authEndpoint: '/broadcasting/auth',
            });

            pusher.connection.bind('state_change', (states) => {
                log('State: ' + states.previous + ' -> ' + states.current, 'info');
            });

            pusher.connection.bind('connected', () => {
                updateStatus(true);
                document.getElementById('socket-id').textContent = pusher.connection.socket_id;
                log('Connected! Socket ID: ' + pusher.connection.socket_id, 'success');
            });

            pusher.connection.bind('disconnected', () => {
                updateStatus(false);
                log('Disconnected', 'error');
            });

            pusher.connection.bind('error', (err) => {
                log('Connection error: ' + JSON.stringify(err), 'error');
            });
        }

        // 1. Public Channel
        function subscribePublic() {
            log('Subscribing to public-test...', 'info');
            publicChannel = pusher.subscribe('public-test');
            publicChannel.bind('pusher:subscription_succeeded', () => {
                log('Subscribed to public-test', 'success');
            });
            publicChannel.bind_global((event, data) => {
                if (event === 'public.event' || (!event.startsWith('pusher:') && !event.startsWith('pusher_internal:'))) {
                    log('PUBLIC EVENT received: ' + event + ' ' + JSON.stringify(data), 'event');
                }
            });
        }

        function sendPublic() {
            fetch('/api/broadcast/public', { method: 'POST', headers: {'Content-Type': 'application/json', 'Accept': 'application/json'}, body: JSON.stringify({message: 'Hello public!'}) })
                .then(r => r.json()).then(d => log('Broadcast sent: ' + JSON.stringify(d)));
        }

        // 2. Private Channel
        function subscribePrivate() {
            privateChannel = pusher.subscribe('private-user.1');
            privateChannel.bind('pusher:subscription_succeeded', () => {
                log('Subscribed to private-user.1', 'success');
            });
            privateChannel.bind('pusher:subscription_error', (err) => {
                log('Private subscription FAILED: ' + JSON.stringify(err), 'error');
            });
            privateChannel.bind_global((event, data) => {
                if (event === 'private.event' || (!event.startsWith('pusher:') && !event.startsWith('pusher_internal:'))) {
                    log('PRIVATE EVENT received: ' + event + ' ' + JSON.stringify(data), 'event');
                }
            });
        }

        function sendPrivate() {
            fetch('/api/broadcast/private', { method: 'POST', headers: {'Content-Type': 'application/json', 'Accept': 'application/json'}, body: JSON.stringify({user_id: 1, message: 'Hello private!'}) })
                .then(r => r.json()).then(d => log('Broadcast sent: ' + JSON.stringify(d)));
        }

        function subscribePrivateNoAuth() {
            const ch = pusher.subscribe('private-secret');
            ch.bind('pusher:subscription_error', (err) => {
                log('Private NO AUTH correctly rejected: ' + JSON.stringify(err), 'success');
            });
            ch.bind('pusher:subscription_succeeded', () => {
                log('Private NO AUTH should have failed!', 'error');
            });
        }

        // 3. Presence Channel
        function subscribePresence() {
            presenceChannel = pusher.subscribe('presence-chat');
            presenceChannel.bind('pusher:subscription_succeeded', (members) => {
                log('Subscribed to presence-chat. Members: ' + members.count, 'success');
                members.each((member) => {
                    log('  Member: ' + member.id + ' - ' + JSON.stringify(member.info), 'info');
                });
            });
            presenceChannel.bind('pusher:member_added', (member) => {
                log('MEMBER JOINED: ' + member.id + ' - ' + JSON.stringify(member.info), 'event');
            });
            presenceChannel.bind('pusher:member_removed', (member) => {
                log('MEMBER LEFT: ' + member.id, 'event');
            });
            presenceChannel.bind('presence.event', (data) => {
                log('PRESENCE EVENT received: ' + JSON.stringify(data), 'event');
            });
            presenceChannel.bind('pusher:subscription_error', (err) => {
                log('Presence subscription FAILED: ' + JSON.stringify(err), 'error');
            });
        }

        function sendPresence() {
            fetch('/api/broadcast/presence', { method: 'POST', headers: {'Content-Type': 'application/json', 'Accept': 'application/json'}, body: JSON.stringify({message: 'Hello presence!'}) })
                .then(r => r.json()).then(d => log('Broadcast sent: ' + JSON.stringify(d)));
        }

        // 4. Whisper
        function sendWhisper() {
            if (!privateChannel) { log('Subscribe to private channel first!', 'error'); return; }
            privateChannel.trigger('client-typing', { user: 'TestUser' });
            log('Whisper sent: client-typing', 'success');
        }

        // 5. Batch
        function sendBatch() {
            fetch('/api/broadcast/batch', { method: 'POST', headers: {'Content-Type': 'application/json', 'Accept': 'application/json'} })
                .then(r => r.json()).then(d => log('Batch sent: ' + JSON.stringify(d)));
        }

        // 6. Unsubscribe
        function unsubscribePublic() {
            if (publicChannel) {
                pusher.unsubscribe('public-test');
                publicChannel = null;
                log('Unsubscribed from public-test', 'success');
            }
        }

        function sendPublicAfterUnsub() {
            fetch('/api/broadcast/public', { method: 'POST', headers: {'Content-Type': 'application/json', 'Accept': 'application/json'}, body: JSON.stringify({message: 'Should NOT receive this'}) })
                .then(r => r.json()).then(d => log('Broadcast sent after unsub (check: should NOT appear as event)', 'info'));
        }

        // 7. Multi-App
        function testMultiApp() {
            pusher2 = new Pusher('app-key-2', {
                wsHost: wsHost,
                wsPort: {{ env("PUSHER_PORT", 6001) }},
                forceTLS: false,
                enabledTransports: ['ws'],
                cluster: '',
            });
            pusher2.connection.bind('connected', () => {
                log('MULTI-APP: Connected with app-key-2, socket: ' + pusher2.connection.socket_id, 'success');
            });
            pusher2.connection.bind('error', (err) => {
                log('MULTI-APP error: ' + JSON.stringify(err), 'error');
            });
        }

        function testInvalidKey() {
            const bad = new Pusher('invalid-key-xxx', {
                wsHost: wsHost,
                wsPort: {{ env("PUSHER_PORT", 6001) }},
                forceTLS: false,
                enabledTransports: ['ws'],
                cluster: '',
            });
            bad.connection.bind('error', (err) => {
                log('INVALID KEY correctly rejected: ' + JSON.stringify(err), 'success');
            });
            bad.connection.bind('connected', () => {
                log('INVALID KEY should have failed!', 'error');
            });
            setTimeout(() => bad.disconnect(), 5000);
        }

        // 8. Metrics
        function fetchMetrics() {
            const metricsUrl = 'http://' + wsHost + ':{{ env("PUSHER_PORT", 6001) }}/metrics';
            fetch(metricsUrl)
                .then(r => r.json())
                .then(d => log('METRICS: ' + JSON.stringify(d, null, 2), 'info'))
                .catch(e => log('Metrics error: ' + e.message, 'error'));
        }

        // 9. Webhooks — logged server-side
        function fetchWebhooks() {
            log('Webhooks are logged in Laravel log file. Check: docker compose logs laravel', 'webhook');
        }

        // 10. Reconnect
        function testDisconnect() {
            pusher.disconnect();
            log('Manually disconnected', 'info');
        }

        function testReconnect() {
            pusher.connect();
            log('Reconnecting...', 'info');
        }

        // Auto-connect on page load
        document.addEventListener('DOMContentLoaded', initPusher);
    </script>
</body>
</html>
