package com.acme.util;

import java.util.function.Function;

/** Static utility with overloads, varargs and lambdas. */
public final class Strings {

    private Strings() {
    }

    public static String join(String separator, String... parts) {
        return String.join(separator, parts);
    }

    public static String join(String separator, Iterable<String> parts) {
        StringBuilder builder = new StringBuilder();
        for (String part : parts) {
            if (builder.length() > 0) {
                builder.append(separator);
            }
            builder.append(part);
        }
        return builder.toString();
    }

    public static String transform(String input, Function<String, String> mapper) {
        return mapper.apply(input);
    }

    public static String transform(String input, Function<String, String> mapper, int times) {
        String current = input;
        for (int i = 0; i < times; i++) {
            current = mapper.apply(current);
        }
        return current;
    }
}
