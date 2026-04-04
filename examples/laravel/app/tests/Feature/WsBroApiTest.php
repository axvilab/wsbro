<?php

namespace Tests\Feature;

use Pusher\Pusher;
use Tests\TestCase;

class WsBroApiTest extends TestCase
{
    private function pusher(?string $key = null, ?string $secret = null, ?string $appId = null): Pusher
    {
        return new Pusher(
            $key ?? config('broadcasting.connections.pusher.key'),
            $secret ?? config('broadcasting.connections.pusher.secret'),
            $appId ?? config('broadcasting.connections.pusher.app_id'),
            config('broadcasting.connections.pusher.options'),
        );
    }

    /**
     * Test: broadcast event to public channel
     */
    public function test_broadcast_to_public_channel(): void
    {
        $pusher = $this->pusher();
        $result = $pusher->trigger('public-test', 'test.event', ['message' => 'hello']);

        $this->assertNotFalse($result);
    }

    /**
     * Test: broadcast to multiple channels
     */
    public function test_broadcast_to_multiple_channels(): void
    {
        $pusher = $this->pusher();
        $result = $pusher->trigger(['channel-1', 'channel-2', 'channel-3'], 'test.event', ['ok' => true]);

        $this->assertNotFalse($result);
    }

    /**
     * Test: batch events
     */
    public function test_batch_events(): void
    {
        $pusher = $this->pusher();
        $batch = [
            ['name' => 'batch.1', 'channel' => 'public-test', 'data' => json_encode(['n' => 1])],
            ['name' => 'batch.2', 'channel' => 'public-test', 'data' => json_encode(['n' => 2])],
        ];

        $result = $pusher->triggerBatch($batch);
        $this->assertNotFalse($result);
    }

    /**
     * Test: get channels list
     */
    public function test_get_channels(): void
    {
        $pusher = $this->pusher();

        // Create a channel by triggering an event (subscriber needed for it to appear)
        $channels = $pusher->getChannels();

        $this->assertIsObject($channels);
    }

    /**
     * Test: multi-app - second app can broadcast
     */
    public function test_multi_app_broadcast(): void
    {
        $pusher2 = $this->pusher('app-key-2', 'app-secret-2', 'app-2');
        $result = $pusher2->trigger('orders', 'new-order', ['id' => 1]);

        $this->assertNotFalse($result);
    }

    /**
     * Test: invalid secret should fail
     */
    public function test_invalid_secret_fails(): void
    {
        $pusher = $this->pusher('app-key-1', 'wrong-secret', 'app-1');

        $this->expectException(\Pusher\ApiErrorException::class);
        $pusher->trigger('test', 'event', ['data' => 'test']);
    }

    /**
     * Test: metrics endpoint returns valid JSON
     */
    public function test_metrics_endpoint(): void
    {
        $host = config('broadcasting.connections.pusher.options.host');
        $port = config('broadcasting.connections.pusher.options.port');

        $response = file_get_contents("http://{$host}:{$port}/metrics");
        $metrics = json_decode($response, true);

        $this->assertArrayHasKey('connections', $metrics);
        $this->assertArrayHasKey('peak_connections', $metrics);
        $this->assertArrayHasKey('total_events', $metrics);
        $this->assertArrayHasKey('channels', $metrics);
    }

    /**
     * Test: prometheus endpoint returns text format
     */
    public function test_prometheus_endpoint(): void
    {
        $host = config('broadcasting.connections.pusher.options.host');
        $port = config('broadcasting.connections.pusher.options.port');

        $response = file_get_contents("http://{$host}:{$port}/metrics/prometheus");

        $this->assertStringContainsString('wsbro_connections', $response);
        $this->assertStringContainsString('wsbro_events_total', $response);
    }

    /**
     * Test: webhook endpoint accepts POST
     */
    public function test_webhook_endpoint(): void
    {
        $response = $this->postJson('/api/wsbro/webhook', [
            'event' => 'connection_established',
            'socket_id' => '123.456',
            'timestamp' => time(),
        ]);

        $response->assertOk()->assertJson(['ok' => true]);
    }

    /**
     * Test: health check endpoint
     */
    public function test_health_check(): void
    {
        $host = config('broadcasting.connections.pusher.options.host');
        $port = config('broadcasting.connections.pusher.options.port');

        $response = file_get_contents("http://{$host}:{$port}/");

        $this->assertStringContainsString('WSBro is running', $response);
    }
}
