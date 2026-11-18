# Security policy

## Supported versions

Security fixes go into the latest minor release. Please upgrade to it before reporting.

## Reporting a vulnerability

Do not open a public issue. Use GitHub's private reporting instead:
**Security → Report a vulnerability** on https://github.com/Milvasoft/pidief.

Please include the version, how pidief is deployed (CLI, Docker, configuration), steps or a file that
reproduces the problem, and the impact you expect. We aim to acknowledge reports within 3 working days and
to publish a fix and an advisory once one is available. We are happy to credit reporters.

## Hardening notes

- Always set `PIDIEF_API_KEYS` in production; without it the API accepts every request.
- Keep `PIDIEF_ALLOW_EXTERNAL=false` unless HTML must load internet resources. With it, a document can make
  Chromium fetch arbitrary URLs (SSRF).
- Run the image as shipped (non-root user) with a read-only root file system, dropped capabilities and a
  memory limit, as in `docker-compose.yml`.
- Chromium runs with `--no-sandbox` in the image because containers usually lack user namespaces. Treat HTML
  from untrusted users accordingly and keep the image up to date.
