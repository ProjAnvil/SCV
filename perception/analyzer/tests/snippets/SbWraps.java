package com.sb.wrap;

import org.springframework.stereotype.Component;

import java.util.Collection;
import java.util.List;
import java.util.Optional;
import java.util.Set;

// §5 collection unwrap: List/Set/Collection take ALL candidates (never
// ambiguous); Optional unwraps with the ambiguity rules applied.
interface WrapSender {
}

@Component
class WrapEmail implements WrapSender {
}

@Component
class WrapSms implements WrapSender {
}

@Component
class Warmer {
}

@Component
class ListConsumer {

    ListConsumer(List<WrapSender> senders) {
    }
}

@Component
class SetConsumer {

    SetConsumer(Set<WrapSender> senders) {
    }
}

@Component
class CollConsumer {

    CollConsumer(Collection<WrapSender> senders) {
    }
}

@Component
class OptionalConsumer {

    OptionalConsumer(Optional<Warmer> warmer) {
    }
}
