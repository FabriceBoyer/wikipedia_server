#!/bin/bash
# Common utils
wget_cmd="wget -q --show-progress --limit-rate=10M"

source $(dirname -- "$0")/.env
out_dir=$DUMP_PATH
mkdir $out_dir

# Wikipedia
$wget_cmd https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream.xml.bz2 -O $out_dir/enwiki-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/enwiki-pages-articles-multistream-index.txt.bz2

# wiktionary
$wget_cmd https://dumps.wikimedia.org/enwiktionary/latest/enwiktionary-latest-pages-articles-multistream.xml.bz2 -O $out_dir/enwiktionary-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwiktionary/latest/enwiktionary-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/enwiktionary-pages-articles-multistream-index.txt.bz2
