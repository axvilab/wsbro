<?php

namespace App\Http\Controllers;

use App\Events\PresenceEvent;
use App\Events\PrivateEvent;
use App\Events\PublicEvent;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Pusher\Pusher;

class BroadcastTestController extends Controller
{
    public function publicEvent(Request $request): JsonResponse
    {
        $message = $request->input('message', 'Hello from public channel');
        broadcast(new PublicEvent($message));

        return response()->json(['status' => 'sent', 'channel' => 'public-test', 'message' => $message]);
    }

    public function privateEvent(Request $request): JsonResponse
    {
        $userId = $request->input('user_id', 1);
        $message = $request->input('message', 'Hello from private channel');
        broadcast(new PrivateEvent($userId, $message));

        return response()->json(['status' => 'sent', 'channel' => "private-user.{$userId}", 'message' => $message]);
    }

    public function presenceEvent(Request $request): JsonResponse
    {
        $message = $request->input('message', 'Hello from presence channel');
        broadcast(new PresenceEvent($message));

        return response()->json(['status' => 'sent', 'channel' => 'presence-chat', 'message' => $message]);
    }

    public function batchEvents(): JsonResponse
    {
        $pusher = new Pusher(
            config('broadcasting.connections.pusher.key'),
            config('broadcasting.connections.pusher.secret'),
            config('broadcasting.connections.pusher.app_id'),
            config('broadcasting.connections.pusher.options'),
        );

        $batch = [
            ['name' => 'batch.first', 'channel' => 'public-test', 'data' => json_encode(['order' => 1])],
            ['name' => 'batch.second', 'channel' => 'public-test', 'data' => json_encode(['order' => 2])],
            ['name' => 'batch.third', 'channel' => 'public-test', 'data' => json_encode(['order' => 3])],
        ];

        $pusher->triggerBatch($batch);

        return response()->json(['status' => 'sent', 'count' => count($batch)]);
    }

    public function channels(): JsonResponse
    {
        $pusher = new Pusher(
            config('broadcasting.connections.pusher.key'),
            config('broadcasting.connections.pusher.secret'),
            config('broadcasting.connections.pusher.app_id'),
            config('broadcasting.connections.pusher.options'),
        );

        $channels = $pusher->getChannels();

        return response()->json(['channels' => $channels]);
    }

    public function metrics(): JsonResponse
    {
        $host = config('broadcasting.connections.pusher.options.host');
        $port = config('broadcasting.connections.pusher.options.port');

        $metrics = json_decode(file_get_contents("http://{$host}:{$port}/metrics"), true);

        return response()->json($metrics);
    }
}
