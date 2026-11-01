<?php

namespace App\Events;

use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Broadcasting\PresenceChannel;
use Illuminate\Contracts\Broadcasting\ShouldBroadcastNow;

class PresenceEvent implements ShouldBroadcastNow
{
    use InteractsWithSockets;

    public function __construct(public string $message) {}

    public function broadcastOn(): PresenceChannel
    {
        return new PresenceChannel('chat');
    }

    public function broadcastAs(): string
    {
        return 'message.sent';
    }
}
