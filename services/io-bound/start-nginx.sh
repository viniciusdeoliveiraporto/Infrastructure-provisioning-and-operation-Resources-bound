#!/bin/bash

ln -s /etc/nginx/sites-available/io-bound /etc/nginx/sites-enabled/io-bound
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx
