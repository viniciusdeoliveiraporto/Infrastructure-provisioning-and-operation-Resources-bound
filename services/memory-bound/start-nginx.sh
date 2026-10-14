#!/bin/bash

ln -s /etc/nginx/sites-available/memory-bound /etc/nginx/sites-enabled/memory-bound
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx
