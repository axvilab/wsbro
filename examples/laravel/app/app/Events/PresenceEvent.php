<?php

namespace App\Events;

use Illuminate\Broadcasting\PresenceChannel;
use Illuminate\Contracts\Broadcasting\ShouldBroadcast;
use Illuminate\Queue\SerializesModels;

class PresenceEvent implements ShouldBroadcast
{
    use SerializesModels;

    public function __construct(
        public string $message,
    ) {}

    public function broadcastOn(): PresenceChannel
    {
        return new PresenceChannel('chat');
    }

    public function broadcastAs(): string
    {
        return 'presence.event';
    }
}
