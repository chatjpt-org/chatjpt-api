#!/bin/sh
set -eu

deploy_dir=${1:-/opt/chatjpt-api}
environment_file="$deploy_dir/.env"
member_models='qwen2.5:1.5b-instruct'
admin_models='gemma3:270m,qwen2.5:0.5b-instruct,gemma3:1b,deepseek-r1:1.5b,llama3.2:3b,qwen3:4b-instruct,gemma3:4b,qwen3.5:4b,deepseek-r1:7b'

if [ "$(id -u)" -ne 0 ]; then
	printf '%s\n' 'Run this script as root.' >&2
	exit 1
fi

sed -i '/^JCHAT_MEMBER_MODELS=/d; /^JCHAT_ADMIN_MODELS=/d' "$environment_file"
printf 'JCHAT_MEMBER_MODELS=%s\n' "$member_models" >>"$environment_file"
printf 'JCHAT_ADMIN_MODELS=%s\n' "$admin_models" >>"$environment_file"

cd "$deploy_dir"
docker compose up -d --build
docker compose exec -T api wget -qO- http://127.0.0.1:8080/healthz
