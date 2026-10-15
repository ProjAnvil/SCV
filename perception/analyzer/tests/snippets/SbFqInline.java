package com.sb.fq;

import org.springframework.stereotype.Component;
import org.springframework.web.bind.annotation.RestController;

// §5: fully-qualified inline annotation form on the nested class, and an
// FQ-written constructor param type (mirrors torture-app GreetingController).
@RestController
class Host {

    @org.springframework.stereotype.Component
    public static class Metrics {

        Metrics(com.sb.fq.Audit audit) {
        }
    }
}

@Component
class Audit {
}
