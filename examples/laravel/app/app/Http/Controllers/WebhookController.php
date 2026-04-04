<?php

namespace App\Http\Controllers;

use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Log;

class WebhookController extends Controller
{
    public function handle(Request $request): JsonResponse
    {
        $event = $request->input('event');
        $channel = $request->input('channel', '');
        $socketId = $request->input('socket_id', '');

        Log::info("WSBro webhook: {$event}", [
            'channel' => $channel,
            'socket_id' => $socketId,
            'data' => $request->input('data'),
            'timestamp' => $request->input('timestamp'),
        ]);

        return response()->json(['ok' => true]);
    }
}
