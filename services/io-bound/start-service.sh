#!/bin/bash

systemctl daemon-reload
systemctl enable --now io-bound
systemctl status io-bound
