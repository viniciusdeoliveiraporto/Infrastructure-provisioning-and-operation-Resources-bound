#!/bin/bash
set -e

cd "$(dirname "$0")"

cp memory-bound /etc/nginx/sites-available/memory-bound
ln -sf /etc/nginx/sites-available/memory-bound /etc/nginx/sites-enabled/memory-bound
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx
