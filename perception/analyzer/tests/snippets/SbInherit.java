package com.sb.inherit;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

// §5: @Autowired field declared in an abstract superclass — the subclass is
// the bean; the edge is "inherited-field".
abstract class BaseAudit {

    @Autowired
    protected Sink sink;
}

@Component
class Sink {
}

@Component
class InheritConsumer extends BaseAudit {
}
