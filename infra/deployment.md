# Deploy na KVM2

O workflow `.github/workflows/deploy.yml` segue o padrao de SSH via Cloudflare
Tunnel usado pelo jpad. Ele nao executa enquanto a repository variable
`DEPLOY_ENABLED` nao for definida como `true`.

## Preparacao manual unica

1. Crie uma chave SSH exclusiva para o deploy e autorize a chave publica para
   `root` na KVM2.
2. Cadastre a chave privada como GitHub Actions secret
   `KVM2_DEPLOY_SSH_PRIVATE_KEY` no repositorio `chatjpt-org/chatjpt-api`.
3. Na KVM2, crie `/opt/chatjpt-api/.env` a partir de `infra/.env.example` e
   preencha senhas, a allowlist `JCHAT_ALLOWED_MODELS` e as credenciais do
   service token `jchat-api-service`.
4. Defina a repository variable `DEPLOY_ENABLED=true` somente depois de testar
   o acesso SSH e revisar o arquivo `.env`.

O `.env` fica somente na KVM2. GitHub Actions nao recebe a senha do PostgreSQL
nem as credenciais do Cloudflare Access do gateway.

## Funcionamento

Depois de um merge na `main`, o workflow conecta em
`ssh.devarthur.com.br` pelo Cloudflare Tunnel, atualiza `/opt/chatjpt-api`,
garante a rede Docker externa `jchat`, executa `docker compose up -d --build`
e consulta `/healthz` dentro do container da API.

Para interromper deploys automaticos sem remover o workflow, defina
`DEPLOY_ENABLED=false` ou remova a variable.
