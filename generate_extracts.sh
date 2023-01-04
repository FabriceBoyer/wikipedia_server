#!/bin/bash

extract_size=100000

source $(dirname -- "$0")/.env
input_dir=$DUMP_PATH
output_dir=$DUMP_PATH/extracts
mkdir -p $output_dir

for i in $input_dir/*.bz2; do
    filename=$(basename -- $i)
    output=$output_dir/${filename%.bz2}
    echo $output
    bzcat $i | head -c $extract_size > $output
done
