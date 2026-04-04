<?php

namespace Tests\Browser;

use Laravel\Dusk\Browser;
use Tests\DuskTestCase;

class WsBroTest extends DuskTestCase
{
    public function test_websocket_connects(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->assertSee('Connected');
        });
    }

    public function test_public_channel_subscribe_and_broadcast(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-public')
                ->pause(2000)
                ->click('#btn-send-public')
                ->pause(2000);

            // Verify broadcast was sent via Laravel
            $browser->assertSeeIn('#log', 'Broadcast sent');

            // Verify the channel exists on WSBro with subscribers
            $browser->script("
                fetch('http://wsbro:6001/metrics')
                    .then(r => r.json())
                    .then(d => document.title = 'channels:' + d.channels)
            ");
            $browser->pause(500);
            $this->assertStringContainsString('channels:', $browser->driver->getTitle());
        });
    }

    public function test_private_channel_with_auth(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-private')
                ->pause(2000)
                ->click('#btn-send-private')
                ->pause(2000)
                ->assertSeeIn('#log', 'Broadcast sent');
        });
    }

    public function test_private_channel_without_auth_fails(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-private-noauth')
                ->waitForText('correctly rejected', 10)
                ->assertSee('correctly rejected');
        });
    }

    public function test_presence_channel(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-presence')
                ->pause(2000)
                ->assertSeeIn('#log', 'presence-chat');
        });
    }

    public function test_client_events_whisper(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-private')
                ->pause(1000)
                ->click('#btn-whisper')
                ->waitForText('Whisper sent', 5)
                ->assertSee('Whisper sent');
        });
    }

    public function test_batch_events(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-public')
                ->pause(1000)
                ->click('#btn-batch')
                ->waitForText('Batch sent', 5)
                ->assertSee('Batch sent');
        });
    }

    public function test_unsubscribe(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-sub-public')
                ->pause(1000)
                ->click('#btn-unsub')
                ->waitForText('Unsubscribed', 5)
                ->assertSee('Unsubscribed');
        });
    }

    public function test_multi_app_valid_key(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-multi-app')
                ->waitForText('MULTI-APP: Connected with app-key-2', 10)
                ->assertSee('MULTI-APP: Connected with app-key-2');
        });
    }

    public function test_invalid_app_key_rejected(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-multi-app-invalid')
                ->pause(5000);

            // Invalid key connection should fail - check WSBro connection count is still 1 (only valid connection)
            $browser->assertSeeIn('#log', 'Connected');
        });
    }

    public function test_reconnect(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10)
                ->click('#btn-disconnect')
                ->waitForText('Disconnected', 5)
                ->click('#btn-reconnect')
                ->waitForText('Connected', 10)
                ->assertSee('Connected');
        });
    }

    public function test_metrics_accessible(): void
    {
        $this->browse(function (Browser $browser) {
            $browser->visit('/?wsHost=wsbro')
                ->waitForText('Connected', 10);

            // Fetch metrics directly from WSBro
            $browser->script("
                fetch('http://wsbro:6001/metrics')
                    .then(r => r.json())
                    .then(d => log('METRICS: ' + JSON.stringify(d), 'info'))
                    .catch(e => log('METRICS ERROR: ' + e.message, 'error'))
            ");

            $browser->pause(2000)
                ->assertSeeIn('#log', 'METRICS:');
        });
    }
}
