<?php

namespace App\Http\Controllers;

use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Broadcast;
use Illuminate\Support\Facades\Cache;
use Pusher\PusherException;

class WebhookController extends Controller
{
    /**
     * Receives WSBro webhooks. They use the Pusher format, so the official SDK verifies the signature.
     */
    public function store(Request $request): JsonResponse
    {
        try {
            $webhook = Broadcast::connection()->getPusher()->webhook(
                [
                    'X-Pusher-Key' => $request->header('X-Pusher-Key'),
                    'X-Pusher-Signature' => $request->header('X-Pusher-Signature'),
                ],
                $request->getContent(),
            );
        } catch (PusherException) {
            return response()->json(['error' => 'invalid signature'], 401);
        }

        $events = Cache::get('wsbro.webhooks', []);
        foreach ($webhook->get_events() as $event) {
            $events[] = (array) $event;
        }
        Cache::forever('wsbro.webhooks', $events);

        return response()->json(['ok' => true]);
    }

    public function index(): JsonResponse
    {
        return response()->json(Cache::get('wsbro.webhooks', []));
    }
}
