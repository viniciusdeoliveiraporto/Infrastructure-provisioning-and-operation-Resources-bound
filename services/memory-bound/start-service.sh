#!/bin/bash

systemctl daemon-reload
systemctl enable --now memory-bound
systemctl status memory-bound
