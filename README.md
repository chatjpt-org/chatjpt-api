# ChatJPT API

Backend Go do ChatJPT. Ele autentica usuarios, guarda conversas no PostgreSQL e
transmite respostas do gateway de IA do home server. O navegador acessa somente
esta API; as credenciais do Cloudflare Access ficam restritas ao servidor.

## Recursos atuais

- Senhas com Argon2id e salt aleatorio.
- Sessoes em cookie `HttpOnly`, `SameSite=Lax` e `Secure` em producao.
- Limite de cinco tentativas de login por usuario e IP em 15 minutos.
- CRUD de conversas e historico de mensagens por usuario.
- Streaming Server-Sent Events (SSE) para o gateway de IA.
- O gateway recebe o ID interno do usuario e nao credenciais do navegador.

Nao existe cadastro publico. O administrador cria usuarios pelo comando
`create-user`.

## Endpoints

| Metodo | Caminho | Descricao |
| --- | --- | --- |
| `GET` | `/healthz` | Verifica a conexao com PostgreSQL. |
| `POST` | `/v1/auth/login` | Cria uma sessao por usuario e senha. |
| `POST` | `/v1/auth/logout` | Revoga a sessao atual. |
| `GET` | `/v1/auth/session` | Retorna o usuario da sessao atual. |
| `GET` | `/v1/conversations` | Lista as conversas do usuario. |
| `POST` | `/v1/conversations` | Cria uma conversa. |
| `GET` | `/v1/conversations/{id}` | Obtem uma conversa. |
| `PATCH` | `/v1/conversations/{id}` | Renomeia uma conversa. |
| `DELETE` | `/v1/conversations/{id}` | Remove uma conversa. |
| `GET` | `/v1/conversations/{id}/messages` | Lista mensagens de uma conversa. |
| `POST` | `/v1/conversations/{id}/messages` | Persiste a mensagem do usuario e transmite a resposta em SSE. |

O corpo do endpoint de mensagens aceita `content` e `max_tokens` (de 1 a
1024; padrao 512). Cada evento SSE tem `delta` e, no ultimo evento, pode ter
`finish_reason`. Ao encerrar com sucesso, a API envia `data: [DONE]`.

Em indisponibilidade do modelo, o stream envia `data: {"error": ...}` com os
codigos `model_busy`, `gateway_unavailable` ou `gateway_error`.

## Configuracao

O modulo requer Go 1.26. `DATABASE_URL` e obrigatoria. Para habilitar IA,
configure as tres variaveis abaixo juntas:

```text
JCHAT_GATEWAY_URL=https://ai.example.com
JCHAT_GATEWAY_ACCESS_ID=<Cloudflare Access client ID>
JCHAT_GATEWAY_ACCESS_SECRET=<Cloudflare Access client secret>
```

As credenciais pertencem ao Service Token `jchat-api-service` no Cloudflare
Access. Nunca devem ser entregues ao cliente web, versionadas ou registradas em
logs.

Para desenvolvimento HTTP local, defina `JCHAT_COOKIE_SECURE=false`. Esse valor
nao deve ser usado na KVM2 em producao.

## Desenvolvimento

```bash
go test ./...
go vet ./...
staticcheck ./...
go run ./cmd/chatjpt-api migrate
go run ./cmd/chatjpt-api create-user
go run ./cmd/chatjpt-api serve
```

## Docker na KVM2

Copie `infra/.env.example` para `.env`, preencha os segredos e crie a rede
externa uma unica vez:

```bash
docker network create jchat
docker compose up -d --build
```

O Compose deixa PostgreSQL fora da rede `jchat`. Apenas a API entra nessa rede,
permitindo que o proxy do cliente web a alcance pelo hostname `chatjpt-api`.
A API nao publica porta no host.
