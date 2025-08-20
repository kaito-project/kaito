#!/bin/sh

if [ -z "$DATA_URLS" ]; then
    echo "No URLs provided in DATA_URLS."
    exit 1
fi
for url in $DATA_URLS; do
    filename=$(basename "$url" | sed 's/[?#].*//')
    echo "Downloading $url to $DATA_VOLUME_PATH/$filename"
    retry_count=0
    while [ $retry_count -lt 3 ]; do
        http_status=$(curl -sSL -w "%{http_code}" -o "$DATA_VOLUME_PATH/$filename" "$url")
        curl_exit_status=$?  # Save the exit status of curl immediately
        if [ "$http_status" -eq "200" ] && [ -s "$DATA_VOLUME_PATH/$filename" ] && [ $curl_exit_status -eq 0 ]; then
            echo "Successfully downloaded $url"
            break
        else
            echo "Failed to download $url, HTTP status code: $http_status, retrying..."
            retry_count=$((retry_count + 1))
            rm -f "$DATA_VOLUME_PATH/$filename" # Remove incomplete file
            sleep 2
        fi
    done
    if [ $retry_count -eq 3 ]; then
        echo "Failed to download $url after 3 attempts"
        exit 1  # Exit with a non-zero status to indicate failure
    fi
done
echo "All downloads completed successfully"
