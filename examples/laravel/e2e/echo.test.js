// End-to-end tests: a real Laravel 13 app broadcasts through WSBro, and laravel-echo + pusher-js
// (the same libraries a browser uses) receive the events.
import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

const LARAVEL = process.env.LARAVEL_URL ?? 'http://localhost:8000';
const HOST = process.env.WSBRO_HOST ?? 'localhost';
const PORT = Number(process.env.WSBRO_PORT ?? 6001);
const BROADCASTER = process.env.BROADCASTER ?? 'reverb';
const TIMEOUT = 5000;

const clients = [];
after(() => clients.forEach((c) => c.disconnect()));

/** Signs in a Laravel user and returns the session cookie. */
async function login(userId) {
  const res = await fetch(`${LARAVEL}/e2e/login/${userId}`, { method: 'POST', headers: { Accept: 'application/json' } });
  assert.equal(res.status, 200, 'login failed');
  return res.headers.getSetCookie().map((c) => c.split(';')[0]).join('; ');
}

/** Creates a connected Echo client, optionally signed in as a Laravel user. */
async function connect({ userId, key = 'app-key-1' } = {}) {
  const cookie = userId ? await login(userId) : '';
  const echo = new Echo({
    broadcaster: BROADCASTER,
    Pusher,
    key,
    cluster: 'mt1',
    wsHost: HOST,
    wsPort: PORT,
    wssPort: PORT,
    forceTLS: false,
    enabledTransports: ['ws'],
    // Same request the browser makes to /broadcasting/auth, with the session cookie.
    channelAuthorization: {
      customHandler: async ({ socketId, channelName }, callback) => {
        const res = await fetch(`${LARAVEL}/broadcasting/auth`, {
          method: 'POST',
          headers: { Accept: 'application/json', Cookie: cookie },
          body: new URLSearchParams({ socket_id: socketId, channel_name: channelName }),
        });
        if (!res.ok) return callback(new Error(`HTTP ${res.status}`), { status: res.status });
        callback(null, await res.json());
      },
    },
  });
  clients.push(echo);

  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('connection timed out')), TIMEOUT);
    echo.connector.pusher.connection.bind('connected', () => { clearTimeout(timer); resolve(); });
  });
  return echo;
}

/** Resolves with the first value passed to the callback registered by `register`. */
function waitFor(register, what = 'event') {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`timed out waiting for ${what}`)), TIMEOUT);
    register((value) => { clearTimeout(timer); resolve(value); });
  });
}

/** Resolves when nothing arrives within the window, rejects if something does. */
function expectNothing(register, ms = 700) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms);
    register((value) => { clearTimeout(timer); reject(new Error('unexpected event: ' + JSON.stringify(value))); });
  });
}

/** Waits until a channel subscription is confirmed by the server. */
function subscribed(channel) {
  return waitFor((done) => channel.subscribed(done), 'subscription');
}

async function laravel(method, path, { body, socketId } = {}) {
  const headers = { Accept: 'application/json', 'Content-Type': 'application/json' };
  if (socketId) headers['X-Socket-ID'] = socketId;
  const res = await fetch(`${LARAVEL}/api${path}`, { method, headers, body: body && JSON.stringify(body) });
  assert.equal(res.status, 200, `${method} ${path} returned ${res.status}: ${await res.clone().text()}`);
  return res.json();
}

test(`[${BROADCASTER}] connects and gets a socket ID`, async () => {
  const echo = await connect();
  assert.match(echo.socketId(), /^\d+\.\d+$/);
});

test(`[${BROADCASTER}] public channel receives broadcast() from Laravel`, async () => {
  const echo = await connect();
  const channel = echo.channel('news');
  await subscribed(channel);

  const received = waitFor((done) => channel.listen('.news.published', done));
  await laravel('POST', '/broadcast/public', { body: { message: 'Hello world' } });

  assert.deepEqual(await received, { message: 'Hello world' });
});

test(`[${BROADCASTER}] toOthers() skips the sender`, async () => {
  const sender = await connect();
  const other = await connect();
  const senderChannel = sender.channel('news');
  const otherChannel = other.channel('news');
  await Promise.all([subscribed(senderChannel), subscribed(otherChannel)]);

  const toOther = waitFor((done) => otherChannel.listen('.news.published', done));
  const toSender = expectNothing((done) => senderChannel.listen('.news.published', done));
  await laravel('POST', '/broadcast/public', { body: { message: 'only others', to_others: true }, socketId: sender.socketId() });

  assert.equal((await toOther).message, 'only others');
  await toSender;
});

test(`[${BROADCASTER}] private channel is authorized through /broadcasting/auth`, async () => {
  const echo = await connect({ userId: 1 });
  const channel = echo.private('App.Models.User.1');
  await subscribed(channel);

  const received = waitFor((done) => channel.listen('.notification.created', done));
  await laravel('POST', '/broadcast/private/1', { body: { message: 'Your order shipped' } });

  const event = await received;
  assert.equal(event.userId, 1);
  assert.equal(event.message, 'Your order shipped');
});

test(`[${BROADCASTER}] private channel of another user is rejected`, async () => {
  const echo = await connect({ userId: 1 });
  const channel = echo.private('App.Models.User.2');

  const error = await waitFor((done) => channel.error(done), 'subscription error');
  assert.equal(error.type, 'AuthError');
  assert.match(error.error, /403/);
});

test(`[${BROADCASTER}] presence channel: here, joining, leaving, whisper and events`, async () => {
  const alice = await connect({ userId: 1 });
  const aliceChat = alice.join('chat');
  const aliceHere = await waitFor((done) => aliceChat.here(done), 'here');
  assert.deepEqual(aliceHere, [{ id: 1, name: 'User 1' }]);

  const joined = waitFor((done) => aliceChat.joining(done), 'joining');
  const bob = await connect({ userId: 2 });
  const bobChat = bob.join('chat');
  const bobHere = await waitFor((done) => bobChat.here(done), 'here');

  assert.deepEqual(await joined, { id: 2, name: 'User 2' });
  assert.deepEqual(bobHere.map((u) => u.id).sort(), [1, 2]);

  const whisper = waitFor((done) => aliceChat.listenForWhisper('typing', done), 'whisper');
  bobChat.whisper('typing', { name: 'User 2' });
  assert.deepEqual(await whisper, { name: 'User 2' });

  const toAlice = waitFor((done) => aliceChat.listen('.message.sent', done));
  const toBob = waitFor((done) => bobChat.listen('.message.sent', done));
  await laravel('POST', '/broadcast/presence', { body: { message: 'Hi chat' } });
  assert.equal((await toAlice).message, 'Hi chat');
  assert.equal((await toBob).message, 'Hi chat');

  const users = await laravel('GET', '/channels');
  assert.deepEqual(users.presence.map((u) => u.id).sort(), ['1', '2']);
  assert.ok(users.channels['presence-chat'].subscription_count >= 2);

  const left = waitFor((done) => aliceChat.leaving(done), 'leaving');
  bob.disconnect();
  assert.deepEqual(await left, { id: 2, name: 'User 2' });
});

test(`[${BROADCASTER}] triggerBatch delivers every event`, async () => {
  const echo = await connect();
  const channel = echo.channel('news');
  await subscribed(channel);

  const first = waitFor((done) => channel.listen('.batch.first', done));
  const second = waitFor((done) => channel.listen('.batch.second', done));
  await laravel('POST', '/broadcast/batch');

  assert.deepEqual(await first, { n: 1 });
  assert.deepEqual(await second, { n: 2 });
});

test(`[${BROADCASTER}] apps are isolated`, async () => {
  const otherApp = await connect({ key: 'app-key-2' });
  const channel = otherApp.channel('news');
  await subscribed(channel);

  const nothing = expectNothing((done) => channel.listen('.news.published', done));
  await laravel('POST', '/broadcast/public', { body: { message: 'app-1 only' } });
  await nothing;
});

test(`[${BROADCASTER}] webhooks pass the official Pusher SDK signature check`, async () => {
  let names = [];
  for (let i = 0; i < 25 && !(names.includes('channel_occupied') && names.includes('member_added')); i++) {
    await new Promise((r) => setTimeout(r, 200));
    names = (await laravel('GET', '/wsbro/webhooks')).map((e) => e.name);
  }
  assert.ok(names.includes('channel_occupied'), 'channel_occupied webhook not received: ' + names);
  assert.ok(names.includes('member_added'), 'member_added webhook not received: ' + names);
});
