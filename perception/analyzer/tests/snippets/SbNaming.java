package com.sb.naming;

import org.springframework.stereotype.Component;

// §5 name rules: explicit value > decapitalized simple name; two leading
// uppercase letters are preserved (URLValidator -> "URLValidator").
@Component
class CacheWarmer {
}

@Component
class URLValidator {
}

@Component("custom")
class Explicit {
}
