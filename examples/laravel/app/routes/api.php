<?php

use App\Http\Controllers\BroadcastTestController;
use App\Http\Controllers\WebhookController;
use Illuminate\Support\Facades\Route;

// Broadcast test endpoints
Route::post('/broadcast/public', [BroadcastTestController::class, 'publicEvent']);
Route::post('/broadcast/private', [BroadcastTestController::class, 'privateEvent']);
Route::post('/broadcast/presence', [BroadcastTestController::class, 'presenceEvent']);
Route::post('/broadcast/batch', [BroadcastTestController::class, 'batchEvents']);
Route::get('/broadcast/channels', [BroadcastTestController::class, 'channels']);
Route::get('/broadcast/metrics', [BroadcastTestController::class, 'metrics']);

// WSBro webhook receiver
Route::post('/wsbro/webhook', [WebhookController::class, 'handle']);
