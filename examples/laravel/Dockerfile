# A fresh Laravel app of the requested major version with the files from ./files on top.
FROM php:8.4-cli

RUN apt-get update \
    && apt-get install -y --no-install-recommends unzip libzip-dev libsqlite3-dev \
    && docker-php-ext-install zip pdo_sqlite \
    && rm -rf /var/lib/apt/lists/*

COPY --from=composer:2 /usr/bin/composer /usr/bin/composer

ARG LARAVEL=13
WORKDIR /var/www/html
RUN composer create-project "laravel/laravel:^${LARAVEL}.0" . --no-interaction --no-scripts --prefer-dist \
    && composer require pusher/pusher-php-server --no-interaction --no-scripts

COPY files/ .
RUN cp .env.example .env \
    && touch database/database.sqlite \
    && composer dump-autoload --optimize \
    && php artisan migrate --force \
    && php artisan --version

EXPOSE 8000
# "php artisan serve" hides container environment variables from the app, so run PHP's server directly.
ENV PHP_CLI_SERVER_WORKERS=4
CMD ["php", "-S", "0.0.0.0:8000", "-t", "public", "public/index.php"]
