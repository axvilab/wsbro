<?php

use App\Models\User;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Route;

Route::get('/', function () {
    // Auto-login test user for broadcasting auth
    $user = User::firstOrCreate(
        ['email' => 'test@wsbro.dev'],
        ['name' => 'Test User', 'password' => bcrypt('password')],
    );
    Auth::login($user);

    return view('test');
});
