Wikipedia server
============

## Introduction

Minimal Wikipedia metadata API server using local dumps

Freely based on https://github.com/d4l3k/wikigopher


## Prerequisites

Use a recent Linux distribution

[Install recent docker CE engine](https://docs.docker.com/engine/install/)


## Configuration

Copy `.env.example` and rename it to `.env`

Modify values as required to store dump files in appropriate location


## Download data

Call `./download_dump.sh` script to download latest dump

Wait for it to finish


## Start server

Call `./start.sh` to start the docker compose stack

Navigate to http://localhost:9095
