<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Contracts\Broadcasting\ShouldBroadcast;
use Illuminate\Queue\SerializesModels;

class PublicEvent implements ShouldBroadcast
{
    use SerializesModels;

    public function __construct(
        public string $message,
    ) {}

    public function broadcastOn(): Channel
    {
        return new Channel('public-test');
    }

    public function broadcastAs(): string
    {
        return 'public.event';
    }
}
