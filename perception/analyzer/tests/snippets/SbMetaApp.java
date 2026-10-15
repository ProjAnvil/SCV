package com.sb.meta;

import org.springframework.boot.autoconfigure.SpringBootApplication;

// §5 jar-carried meta chain via the table: @SpringBootApplication ->
// SpringBootConfiguration -> @Configuration.
@SpringBootApplication
class ChainApplication {
}

@MyRestController
class MetaController {
}
