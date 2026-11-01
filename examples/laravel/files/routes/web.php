<?php

use App\Models\User;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Route;

Route::get('/', fn () => view('welcome'));

// Signs in a test user, so the end-to-end suite can authorize private and presence channels.
Route::post('/e2e/login/{id}', function (int $id) {
    abort_unless(app()->environment('local', 'testing'), 404);

    $user = User::find($id) ?? User::forceCreate([
        'id' => $id,
        'name' => "User {$id}",
        'email' => "user{$id}@wsbro.test",
        'password' => bcrypt('password'),
    ]);
    Auth::login($user);

    return ['id' => $user->id, 'name' => $user->name];
});
