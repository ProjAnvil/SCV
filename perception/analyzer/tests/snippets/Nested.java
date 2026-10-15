package com.example.app;

public class Nested {

    public static class Inner {

        public interface Listener {
        }

        // doubly nested -> Nested$Inner$Deep
        public static class Deep {
        }
    }
}
