# Laravel

WSBro speaks the Pusher protocol, which is also what Laravel Reverb speaks. Laravel and Laravel Echo
do not need any package or code from WSBro: you point the existing `reverb` or `pusher` broadcaster at it.

Tested end to end on every change with **Laravel 12 and 13** (also verified on 11), both the `reverb`
and the `pusher` broadcaster, `laravel-echo` 2 and `pusher-js` 8. See [the test suite](../examples/laravel).

## Option 1: replace Reverb (recommended)

If your app was set up with `php artisan install:broadcasting` and Reverb, keep everything as is and
run WSBro instead of `php artisan reverb:start`.

1. Start WSBro with the same credentials Laravel uses:

   ```bash
   docker run -d --name wsbro -p 6001:6001 \
     -e WSBRO_APP_ID="$REVERB_APP_ID" \
     -e WSBRO_APP_KEY="$REVERB_APP_KEY" \
     -e WSBRO_APP_SECRET="$REVERB_APP_SECRET" \
     ghcr.io/axvilab/wsbro
   ```

2. Point Laravel at it in `.env`:

   ```env
   BROADCAST_CONNECTION=reverb

   REVERB_APP_ID=my-app
   REVERB_APP_KEY=my-key
   REVERB_APP_SECRET=my-secret
   REVERB_HOST=127.0.0.1      # where Laravel reaches WSBro
   REVERB_PORT=6001
   REVERB_SCHEME=http

   VITE_REVERB_APP_KEY="${REVERB_APP_KEY}"
   VITE_REVERB_HOST=ws.example.com   # where browsers reach WSBro
   VITE_REVERB_PORT=443
   VITE_REVERB_SCHEME=https
   ```

3. Rebuild the frontend (`npm run build`). Echo keeps using `broadcaster: 'reverb'`.

You can remove the `laravel/reverb` package afterwards: the `reverb` broadcaster itself is part of
the Laravel framework.

## Option 2: the Pusher broadcaster

```bash
composer require pusher/pusher-php-server
```

```env
BROADCAST_CONNECTION=pusher

PUSHER_APP_ID=my-app
PUSHER_APP_KEY=my-key
PUSHER_APP_SECRET=my-secret
PUSHER_HOST=127.0.0.1
PUSHER_PORT=6001
PUSHER_SCHEME=http
PUSHER_APP_CLUSTER=mt1

VITE_PUSHER_APP_KEY="${PUSHER_APP_KEY}"
VITE_PUSHER_HOST=ws.example.com
VITE_PUSHER_PORT=443
VITE_PUSHER_SCHEME=https
VITE_PUSHER_APP_CLUSTER="${PUSHER_APP_CLUSTER}"
```

```js
import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

window.Pusher = Pusher;
window.Echo = new Echo({
    broadcaster: 'pusher',
    key: import.meta.env.VITE_PUSHER_APP_KEY,
    cluster: import.meta.env.VITE_PUSHER_APP_CLUSTER,
    wsHost: import.meta.env.VITE_PUSHER_HOST,
    wsPort: import.meta.env.VITE_PUSHER_PORT,
    wssPort: import.meta.env.VITE_PUSHER_PORT,
    forceTLS: import.meta.env.VITE_PUSHER_SCHEME === 'https',
    enabledTransports: ['ws', 'wss'],
});
```

## Everything Laravel broadcasting does works

| Feature | Laravel API | Notes |
|---|---|---|
| Public channels | `new Channel('orders')`, `Echo.channel()` | |
| Private channels | `new PrivateChannel(...)`, `Echo.private()` | authorized by your `routes/channels.php` |
| Presence channels | `new PresenceChannel(...)`, `Echo.join().here().joining().leaving()` | a user with several tabs counts once |
| Encrypted channels | `PrivateEncryptedChannel`, `Echo.encryptedPrivate()` | WSBro relays the ciphertext, it never sees the key |
| Client events | `.whisper()`, `.listenForWhisper()` | private and presence channels only, sender must be subscribed |
| Skip the sender | `broadcast($event)->toOthers()` | uses `X-Socket-ID`, as with Pusher |
| Batch events | `$pusher->triggerBatch()` | all or nothing |
| Channel info | `$pusher->getChannels()`, `getChannelInfo()`, `getPresenceUsers()` | |
| Webhooks | `$pusher->webhook($headers, $body)` | signed exactly like Pusher webhooks |
| User authentication | `pusher.signin()`, `$pusher->sendToUser()` | |
| Terminate user connections | `$pusher->terminateUserConnections($id)` | |
| Cache channels | `cache-*` channels | last event replayed to new subscribers |

## Receiving webhooks

WSBro sends Pusher-format webhooks signed with the app secret, so the official SDK verifies them:

```php
Route::post('/wsbro/webhook', function (Request $request) {
    $webhook = Broadcast::connection()->getPusher()->webhook(
        [
            'X-Pusher-Key' => $request->header('X-Pusher-Key'),
            'X-Pusher-Signature' => $request->header('X-Pusher-Signature'),
        ],
        $request->getContent(),
    ); // throws PusherException when the signature is wrong

    foreach ($webhook->get_events() as $event) {
        // $event->name: channel_occupied, channel_vacated, member_added, member_removed
    }
});
```

Configure the URL with `webhook_url` per app or `WSBRO_WEBHOOK_URL` globally. See [webhooks](webhooks.md).

## Queues

`ShouldBroadcast` events go through your queue. If events "do not arrive", make sure a worker is running
(`php artisan queue:work`) or use `ShouldBroadcastNow` for events that must go out immediately.

## Troubleshooting

Open the [dashboard](dashboard.md) and its **Debug console**: it shows every connection, subscription,
failed authorization and event as it happens. If an event is not listed there, it never left Laravel.

| Symptom | Likely cause |
|---|---|
| `connected`, but private channels fail with 403 | `routes/channels.php` returns false, or the user is not logged in |
| Private channel fails with "Invalid signature" | Laravel and WSBro use different app secrets |
| Browser cannot connect | `VITE_*_HOST`/`PORT` point to an address the browser cannot reach, or `WSBRO_ALLOWED_ORIGINS` blocks the site |
| Events from `php artisan serve` never arrive | `artisan serve` hides container environment variables; check `php artisan config:show broadcasting` |
