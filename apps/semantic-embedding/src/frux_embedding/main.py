import uvicorn

from .app import app, settings


def main() -> None:
    uvicorn.run(
        app,
        host=settings.bind_host,
        port=settings.port,
        workers=1,
        log_level=settings.log_level.lower(),
        access_log=False,
        server_header=False,
        date_header=False,
    )


if __name__ == "__main__":
    main()
