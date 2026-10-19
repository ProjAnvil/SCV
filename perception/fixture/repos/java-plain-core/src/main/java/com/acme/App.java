package com.acme;

import com.acme.algo.MutualRecursion;
import com.acme.core.model.Graph;
import com.acme.core.service.AsyncDispatcher;
import com.acme.core.service.Dispatcher;
import com.acme.util.Strings;

import java.util.concurrent.Executors;

/** Plain Java entry point: no Spring anywhere. */
public class App {

    public static void main(String[] args) {
        Graph.Builder<String> builder = new Graph.Builder<>();
        builder.edge("a", "b").edge("b", "c");
        Graph<String> graph = builder.build();
        System.out.println(graph.depthFirst("a"));

        Dispatcher<String> dispatcher = new AsyncDispatcher<>(Executors.newSingleThreadExecutor());
        dispatcher.dispatch(Strings.join(",", "x", "y"));

        System.out.println(MutualRecursion.isEven(10));
    }
}
