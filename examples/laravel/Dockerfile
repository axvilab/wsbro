FROM php:8.4-cli

RUN apt-get update && apt-get install -y \
    unzip \
    git \
    libzip-dev \
    libsqlite3-dev \
    && docker-php-ext-install zip pdo_sqlite \
    && rm -rf /var/lib/apt/lists/*

COPY --from=composer:2 /usr/bin/composer /usr/bin/composer

WORKDIR /var/www/html

COPY app/composer.json app/composer.lock ./
RUN composer install --no-scripts --no-autoloader

COPY app/ .
RUN composer dump-autoload --optimize

RUN touch database/database.sqlite \
    && php artisan migrate --force

EXPOSE 8000

CMD ["php", "artisan", "serve", "--host=0.0.0.0", "--port=8000"]
