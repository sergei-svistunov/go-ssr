const path = require('path');
const {CleanWebpackPlugin} = require('clean-webpack-plugin');
const MiniCssExtractPlugin = require('mini-css-extract-plugin');
const GoSSRAssetsPlugin = require('gossr-assets-webpack-plugin');

module.exports = {
    entry: {},
    output: {
        filename: 'js/[name].[chunkhash].js',
        path: path.resolve(__dirname, 'static'),
        publicPath: '/static/'
    },
    stats: {warnings: false},
    cache: {
        type: 'filesystem'
    },
    module: {
        rules: [
            {
                test: /\.ts$/,
                use: 'ts-loader',
                exclude: /node_modules/,
            },
            {
                test: /\.scss$/,
                use: [
                    MiniCssExtractPlugin.loader,
                    'css-loader',
                    'sass-loader',
                ],
            },
            {
                test: /\.(png|jpe?g|gif|svg)$/i,
                type: 'asset/resource',
                generator: {
                    filename: (pathData) => {
                        const newPath = pathData.module.resourceResolveData.relativePath
                            .replace(/^\.?\/pages\//, '');
                        return `images/${newPath}[name].[hash][ext]`;
                    }
                },
            },
        ],
    },
    resolve: {
        extensions: ['.ts', '.js'],
    },
    plugins: [
        new GoSSRAssetsPlugin(),
        new CleanWebpackPlugin(),
        new MiniCssExtractPlugin({
            filename: 'css/[name].[chunkhash].css',
        }),
    ],
    optimization: {
        splitChunks: {
            chunks: 'all',
            hidePathInfo: true,
            cacheGroups: {
                bootstrap: {
                    test: /[\\/]node_modules[\\/]bootstrap[\\/]/,
                    name: 'bootstrap',
                    chunks: 'all',
                    enforce: true,
                },
                // Separate Luxon into its own chunk
                luxon: {
                    test: /[\\/]node_modules[\\/]luxon[\\/]/,
                    name: 'luxon',
                    chunks: 'all',
                    enforce: true,
                },
                // Put all other node_modules into a vendor chunk
                vendors: {
                    test: /[\\/]node_modules[\\/](?!bootstrap|luxon)/,  // Exclude bootstrap and luxon
                    name: 'vendors',
                    chunks: 'all',
                    enforce: true,
                },
            },
        },
    },
}