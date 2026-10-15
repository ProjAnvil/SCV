package com.sb.resolve;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Primary;
import org.springframework.stereotype.Component;

// §5 resolution order: qualifier, @Primary collapse, by-name fallback,
// honest ambiguity. The Channel family carries a @Primary; the Linker family
// does not, so by-name/ambiguity rules are reachable.
interface Channel {
}

@Component
class EmailChannel implements Channel {
}

@Primary
@Component
class SmsChannel implements Channel {
}

interface Linker {
}

@Component
class AlphaLinker implements Linker {
}

@Component
class BetaLinker implements Linker {
}

@Component
class PrimaryConsumer {

    PrimaryConsumer(Channel channel) {
    }
}

@Component
class ByNameConsumer {

    @Autowired
    private Linker alphaLinker;
}

@Component
class AmbiguousConsumer {

    @Autowired
    private Linker fallback;
}
