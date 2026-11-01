<?php

use App\Events\PresenceEvent;
use App\Events\PrivateEvent;
use App\Events\PublicEvent;
use App\Http\Controllers\WebhookController;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Broadcast;
use Illuminate\Support\Facades\Route;

// broadcast()->toOthers() reads the X-Socket-ID header, exactly as with Laravel Echo + axios.
Route::post('/broadcast/public', function (Request $request) {
    $event = broadcast(new PublicEvent($request->input('message', 'Hello')));
    if ($request->boolean('to_others')) {
        $event->toOthers();
    }

    return ['sent' => true];
});

Route::post('/broadcast/private/{userId}', function (Request $request, int $userId) {
    broadcast(new PrivateEvent($userId, $request->input('message', 'Hello')));

    return ['sent' => true];
});

Route::post('/broadcast/presence', function (Request $request) {
    broadcast(new PresenceEvent($request->input('message', 'Hello')));

    return ['sent' => true];
});

Route::post('/broadcast/batch', function () {
    Broadcast::connection()->getPusher()->triggerBatch([
        ['channel' => 'news', 'name' => 'batch.first', 'data' => ['n' => 1]],
        ['channel' => 'news', 'name' => 'batch.second', 'data' => ['n' => 2]],
    ]);

    return ['sent' => true];
});

Route::get('/channels', function () {
    $pusher = Broadcast::connection()->getPusher();

    return [
        'channels' => $pusher->getChannels(['info' => 'subscription_count'])->channels,
        'presence' => $pusher->getPresenceUsers('presence-chat')->users,
    ];
});

Route::post('/wsbro/webhook', [WebhookController::class, 'store']);
Route::get('/wsbro/webhooks', [WebhookController::class, 'index']);
