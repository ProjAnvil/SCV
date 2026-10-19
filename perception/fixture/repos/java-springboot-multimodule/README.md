# billing-platform (perception fixture)

Gradle multi-module Spring Boot layout. The root project has no sources and no
build marker of its own; the Boot entry point sits in the `app` submodule, and
the injection chain crosses module boundaries:

    app:InvoiceController -> core:InvoiceService -> infra:LedgerClient

This is the layout that used to defeat framework detection, which only looked
at the root `src/main/java`.
