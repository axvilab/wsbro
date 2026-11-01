<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Contracts\Broadcasting\ShouldBroadcastNow;

class PublicEvent implements ShouldBroadcastNow
{
    use InteractsWithSockets;

    public function __construct(public string $message) {}

    public function broadcastOn(): Channel
    {
        return new Channel('news');
    }

    public function broadcastAs(): string
    {
        return 'news.published';
    }
}
