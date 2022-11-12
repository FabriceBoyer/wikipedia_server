#!/bin/bash

wget -q --limit-rate=10M --show-progress https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream.xml.bz2 -O $(dirname -- "$0")/dump/enwiki-pages-articles-multistream.xml.bz2
wget -q --limit-rate=10M --show-progress https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream-index.txt.bz2 -O $(dirname -- "$0")/dump/enwiki-pages-articles-multistream-index.txt.bz2

