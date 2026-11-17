"""Permission preparation shared by the production release and Linux regression."""


def prepare_migrator(run, container, path):
    # Windows release archives can preserve0644/0666. Normalize after docker cp
    # so both local-build and BuildOnServer artifacts execute without world-write.
    run('docker', 'exec', container, 'chmod', '0755', path)
