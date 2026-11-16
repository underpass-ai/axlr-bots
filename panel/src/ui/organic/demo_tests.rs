//! The organic glow drawn headless on a small scene (a card, a list of
//! five rows, four tiles) in both themes, and measured on the pixels:
//! where it is, where it must not be, how strong and how smooth.
//! Not part of the suite, it needs a renderer that draws:
//! `ICED_TEST_BACKEND=tiny-skia cargo test organic_shots -- --ignored`.
//! It writes `organic-<name>.png` into `ORGANIC_SHOTS` (by default
//! `target/organic-shots`).

use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use iced::advanced::graphics::image as pictures;
use iced::widget::{Space, column, container, image, row, text};
use iced::{Alignment, Background, Border, Element, Length, Padding, Point, Rectangle, Size};
use iced_test::Simulator;

use super::*;
use crate::ui::styles::{self, RADIUS, RADIUS_S};
use crate::ui::view::widgets::{REGULAR, SEMIBOLD};
use crate::ui::view_tests::{panel_settings, turn};
use crate::ui::{Tokens, Tone};

const WINDOW: Size = Size::new(960.0, 360.0);
const CARD: Rectangle = Rectangle {
    x: 20.0,
    y: 20.0,
    width: 420.0,
    height: 230.0,
};
const ROW_HEIGHT: f32 = 36.0;
const TILE: Size = Size::new(96.0, 26.0);
const BOTS: [&str; 5] = ["researcher", "reviewer", "builder", "watcher", "planner"];

/// The row `index` of the list: inside the list card's hairline, under
/// its header.
fn row_bounds(index: usize) -> Rectangle {
    Rectangle {
        x: 457.0,
        y: 21.0 + 30.0 + index as f32 * ROW_HEIGHT,
        width: 482.0,
        height: ROW_HEIGHT,
    }
}

fn tile_bounds(index: usize) -> Rectangle {
    Rectangle {
        x: 20.0 + index as f32 * (TILE.width + 8.0),
        y: 294.0,
        width: TILE.width,
        height: TILE.height,
    }
}

fn card_surface() -> Surface {
    Surface::of("card")
}

fn row_surface(index: usize) -> Surface {
    Surface::of(("row", index))
}

fn tile_surface(index: usize) -> Surface {
    Surface::of(("tile", index))
}

/// The scene: every surface wrapped as a view of the panel would.
fn scene(hover: &Hover, tokens: Tokens) -> Element<'_, Signal> {
    let tint = Tint::of(tokens);
    let muted = |words: &'static str| text(words).size(13).color(tokens.muted).font(REGULAR);
    let strong = |words: &'static str| text(words).size(14).color(tokens.text).font(SEMIBOLD);
    let pill = |tone: Tone, words: &'static str| {
        container(
            text(words)
                .size(13)
                .font(SEMIBOLD)
                .color(styles::pill_ink(tokens, tone)),
        )
        .padding(Padding::from([2, 8]))
        .style(styles::pill(tokens, tone))
    };

    let card = container(
        backdrop(
            hover,
            card_surface(),
            tint,
            container(
                column![
                    strong("builder"),
                    muted("Builds the change a reviewer asked for, on its own branch."),
                    row![
                        pill(Tone::Busy, "Running"),
                        pill(Tone::Wait, "Needs OK"),
                        pill(Tone::Good, "Done")
                    ]
                    .spacing(8),
                    muted("Started by an event · 14:02 · 3 min ago"),
                    text("The words stay readable over the glow.")
                        .size(14)
                        .color(tokens.text),
                ]
                .spacing(10),
            )
            .padding(16)
            .width(Length::Fill)
            .height(Length::Fill),
            |signal| signal,
        )
        .radius(RADIUS)
        .inset(1.0),
    )
    .style(styles::card(tokens))
    .width(CARD.width)
    .height(CARD.height);

    let tiles = row(BOTS.iter().take(4).enumerate().map(|(index, name)| {
        container(
            backdrop(
                hover,
                tile_surface(index),
                tint,
                container(text(*name).size(12).color(tokens.text).font(REGULAR))
                    .padding(Padding::from([0, 8]))
                    .align_y(Alignment::Center)
                    .width(Length::Fill)
                    .height(Length::Fill),
                |signal| signal,
            )
            .radius(RADIUS_S)
            .inset(1.0),
        )
        .style(move |_| container::Style {
            background: Some(Background::Color(tokens.surface)),
            border: Border {
                color: tokens.border,
                width: 1.0,
                radius: RADIUS_S.into(),
            },
            ..container::Style::default()
        })
        .width(TILE.width)
        .height(TILE.height)
        .into()
    }))
    .spacing(8);

    let header = container(muted("STATUS    BOT    WHAT"))
        .padding(Padding::from([0, 14]))
        .align_y(Alignment::Center)
        .width(Length::Fill)
        .height(30)
        .style(move |_| container::Style {
            background: Some(Background::Color(tokens.raised)),
            ..container::Style::default()
        });
    let rows = BOTS.iter().enumerate().map(|(index, name)| {
        backdrop(
            hover,
            row_surface(index),
            tint,
            container(
                row![
                    container(strong(name)).width(110),
                    muted("Review the fix before its pull request opens"),
                    Space::new().width(Length::Fill),
                    muted("3 min"),
                ]
                .spacing(12)
                .align_y(Alignment::Center),
            )
            .padding(Padding::from([0, 14]))
            .align_y(Alignment::Center)
            .width(Length::Fill)
            .height(ROW_HEIGHT),
            |signal| signal,
        )
        .into()
    });
    let list = container(column![header].extend(rows).push(Space::new().height(8)))
        .padding(1)
        .style(styles::card(tokens))
        .width(Length::Fill);

    container(
        row![
            column![
                card,
                Space::new().height(16),
                container(muted("FLEET")).height(20),
                Space::new().height(8),
                tiles
            ]
            .width(CARD.width),
            list,
        ]
        .spacing(16),
    )
    .padding(20)
    .width(Length::Fill)
    .height(Length::Fill)
    .style(move |_| container::Style {
        background: Some(Background::Color(tokens.page)),
        ..container::Style::default()
    })
    .into()
}

/// The pointer has been on this surface, at this place, long enough for
/// its glow to be whole, and the waves are `seconds` old.
fn lit(surface: Surface, bounds: Rectangle, at: Point, seconds: f32) -> Hover {
    let start = Instant::now();
    let mut hover = Hover::default();
    let over = |when| Signal::Over {
        surface,
        at,
        size: bounds.size(),
        when,
    };
    hover.signal(over(start));
    let now = start + Duration::from_secs_f32(seconds);
    hover.signal(over(now - Duration::from_millis(400)));
    hover.tick(now);
    assert_eq!(hover.shine(surface).map(|shine| shine.fade), Some(1.0));
    hover
}

/// The point of the window, in points, that is `at` (0 to 1 each way) of
/// the surface.
fn place(bounds: Rectangle, at: Point) -> Point {
    Point::new(bounds.x + at.x * bounds.width, bounds.y + at.y * bounds.height)
}

/// A drawing of the scene, two pixels to the point.
struct Shot {
    width: usize,
    height: usize,
    rgba: Vec<u8>,
}

impl Shot {
    /// Draws the scene and, with a name, keeps the drawing as a PNG.
    fn take(hover: &Hover, tokens: Tokens, name: Option<&str>) -> Self {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, scene(hover, tokens));
        let snapshot = ui.snapshot(&tokens.theme()).unwrap();
        // `matches_image` writes a new file named after the renderer, so
        // it writes into an empty directory and the file is read there.
        let scratch = tempfile::tempdir().unwrap();
        assert!(snapshot.matches_image(scratch.path().join("shot")).unwrap());
        let drawn = fs::read_dir(scratch.path()).unwrap().next().unwrap().unwrap().path();
        if let Some(name) = name {
            let _ = fs::copy(&drawn, shots_dir().join(format!("organic-{name}.png"))).unwrap();
        }
        let picture = pictures::load(&image::Handle::from_path(&drawn)).unwrap();
        Self {
            width: picture.width() as usize,
            height: picture.height() as usize,
            rgba: picture.as_raw().to_vec(),
        }
    }

    /// The colour of the pixel at this place, in pixels.
    fn pixel(&self, x: usize, y: usize) -> [i32; 3] {
        let at = (y * self.width + x) * 4;
        [
            i32::from(self.rgba[at]),
            i32::from(self.rgba[at + 1]),
            i32::from(self.rgba[at + 2]),
        ]
    }

    /// How far this pixel is from the same one of another drawing: the
    /// biggest difference of its three colours.
    fn change(&self, other: &Shot, x: usize, y: usize) -> i32 {
        let (a, b) = (self.pixel(x, y), other.pixel(x, y));
        (0..3).map(|channel| (a[channel] - b[channel]).abs()).max().unwrap_or(0)
    }

    /// The same, at a place given in points.
    fn change_at(&self, other: &Shot, at: Point) -> i32 {
        self.change(other, (at.x * 2.0) as usize, (at.y * 2.0) as usize)
    }
}

fn shots_dir() -> PathBuf {
    let out = std::env::var_os("ORGANIC_SHOTS")
        .map(PathBuf::from)
        .unwrap_or_else(|| Path::new(env!("CARGO_MANIFEST_DIR")).join("target/organic-shots"));
    fs::create_dir_all(&out).unwrap();
    out
}

/// Whether the place (in pixels) is inside the surface's rounded shape,
/// shrunk by `margin` points.
fn inside(bounds: Rectangle, radius: f32, margin: f32, x: usize, y: usize) -> bool {
    let (x, y) = ((x as f32 + 0.5) / 2.0, (y as f32 + 0.5) / 2.0);
    let shape = bounds.shrink(margin);
    let radius = (radius - margin).max(0.0);
    if !shape.contains(Point::new(x, y)) {
        return false;
    }
    let nearest = |at: f32, from: f32, length: f32| at.clamp(from + radius, from + length - radius);
    let (cx, cy) = (nearest(x, shape.x, shape.width), nearest(y, shape.y, shape.height));
    (x - cx).hypot(y - cy) <= radius
}

/// What the glow of one surface must look like on the pixels, against
/// the same scene without it. `centred` is for a pointer well inside
/// the surface, where none of the glow is cut away: its middle is then
/// near the pointer.
fn check(
    lit: &Shot,
    plain: &Shot,
    (bounds, radius): (Rectangle, f32),
    pointer: Point,
    halo: Halo,
    centred: bool,
    what: &str,
) {
    assert_eq!((lit.width, lit.height), (plain.width, plain.height));
    let (mut strongest, mut sum, mut sum_x, mut sum_y) = (0, 0.0, 0.0, 0.0);
    for y in 0..lit.height {
        for x in 0..lit.width {
            let change = lit.change(plain, x, y);
            if inside(bounds, radius, -0.75, x, y) {
                strongest = strongest.max(change);
                let weight = f64::from(change);
                sum += weight;
                sum_x += weight * x as f64 / 2.0;
                sum_y += weight * y as f64 / 2.0;
            } else {
                assert_eq!(change, 0, "{what}: the glow spills at {},{}", x / 2, y / 2);
            }
        }
    }
    // Visible, and never more than the halo's peak of the way from the
    // ground to the tint's farthest colour.
    assert!(strongest >= 12, "{what}: the glow hardly shows ({strongest})");
    assert!(
        strongest as f32 <= halo.peak * 255.0 + 3.0,
        "{what}: too strong ({strongest})"
    );
    // Under the pointer: the glow's middle is near it.
    let middle = Point::new((sum_x / sum) as f32, (sum_y / sum) as f32);
    let reach = halo.reach * bounds.height * (bounds.width / bounds.height * 0.5).max(1.0);
    let off = (middle.x - pointer.x).hypot(middle.y - pointer.y);
    assert!(
        !centred || off <= reach * 0.4,
        "{what}: the glow's middle is {off} points from the pointer"
    );
    // And it shows right there, around whatever word is under it.
    let around = [(-4.0, -4.0), (4.0, -4.0), (0.0, 0.0), (-4.0, 4.0), (4.0, 4.0)];
    let under = around
        .iter()
        .map(|(dx, dy)| lit.change_at(plain, Point::new(pointer.x + dx, pointer.y + dy)))
        .max()
        .unwrap_or(0);
    assert!(under >= 6, "{what}: nothing under the pointer");
}

/// No blocks, no hard edge: along a line of the surface with no words on
/// it, from one pixel to the next the colour moves by a step at most.
fn check_smooth(lit: &Shot, from: Point, length: f32, what: &str) {
    let y = (from.y * 2.0) as usize;
    let mut moved = 0;
    for x in (from.x * 2.0) as usize + 1..((from.x + length) * 2.0) as usize {
        let (a, b) = (lit.pixel(x - 1, y), lit.pixel(x, y));
        let step = (0..3).map(|channel| (a[channel] - b[channel]).abs()).max().unwrap_or(0);
        assert!(step <= 2, "{what}: a step of {step} at {}", x / 2);
        moved += step;
    }
    assert!(moved >= 8, "{what}: the line is flat, the glow is not on it");
}

#[test]
#[ignore = "draws with a renderer and writes PNGs; run it with --ignored"]
fn organic_shots() {
    for tokens in [Tokens::LIGHT, Tokens::DARK] {
        let theme = if tokens.dark { "dark" } else { "light" };
        let plain = Shot::take(&Hover::new(Motion::Off), tokens, None);

        // The card, at three moments of its waves.
        let at = Point::new(0.62, 0.55);
        for (shot, seconds) in [(1, 2.0), (2, 9.0), (3, 17.0)] {
            let hover = lit(card_surface(), CARD, at, seconds);
            let drawn = Shot::take(&hover, tokens, Some(&format!("card-{theme}-{shot}")));
            let what = format!("card, {theme}, {seconds} s");
            check(&drawn, &plain, (CARD, RADIUS), place(CARD, at), Halo::WIDE, true, &what);
            // The line under the card's words, through the glow.
            check_smooth(&drawn, Point::new(CARD.x + 40.0, CARD.y + 205.0), 340.0, &what);
        }

        // A row, and its glow along the row.
        let (bounds, at) = (row_bounds(2), Point::new(0.35, 0.5));
        let hover = lit(row_surface(2), bounds, at, 9.0);
        let drawn = Shot::take(&hover, tokens, Some(&format!("row-{theme}")));
        let what = format!("row, {theme}");
        check(
            &drawn,
            &plain,
            (bounds, 0.0),
            place(bounds, at),
            Halo::NEAR,
            true,
            &what,
        );
        check_smooth(&drawn, Point::new(bounds.x + 4.0, bounds.y + 3.0), 470.0, &what);

        // A tile.
        let (bounds, at) = (tile_bounds(1), Point::new(0.5, 0.5));
        let hover = lit(tile_surface(1), bounds, at, 17.0);
        let drawn = Shot::take(&hover, tokens, Some(&format!("tile-{theme}")));
        let what = format!("tile, {theme}");
        check(
            &drawn,
            &plain,
            (bounds, RADIUS_S),
            place(bounds, at),
            Halo::NEAR,
            true,
            &what,
        );

        // The pointer in the card's far corner: the glow reaches the
        // edges there and stays inside the rounded corner.
        let at = Point::new(0.985, 0.97);
        let hover = lit(card_surface(), CARD, at, 5.0);
        let drawn = Shot::take(&hover, tokens, Some(&format!("corner-{theme}")));
        let what = format!("card's corner, {theme}");
        check(
            &drawn,
            &plain,
            (CARD, RADIUS),
            place(CARD, at),
            Halo::WIDE,
            false,
            &what,
        );
        let (right, bottom) = (CARD.x + CARD.width, CARD.y + CARD.height);
        for edge in [
            Point::new(right - 1.75, bottom - 30.0),
            Point::new(right - 30.0, bottom - 1.75),
        ] {
            assert!(
                drawn.change_at(&plain, edge) >= 6,
                "{what}: no glow by the edge at {edge:?}"
            );
        }

        // The pointer at a row's far end: the glow reaches the end.
        let (bounds, at) = (row_bounds(4), Point::new(0.995, 0.9));
        let hover = lit(row_surface(4), bounds, at, 5.0);
        let drawn = Shot::take(&hover, tokens, Some(&format!("edge-{theme}")));
        let what = format!("row's end, {theme}");
        check(
            &drawn,
            &plain,
            (bounds, 0.0),
            place(bounds, at),
            Halo::NEAR,
            false,
            &what,
        );
        let end = Point::new(bounds.x + bounds.width - 0.25, bounds.y + bounds.height - 0.25);
        assert!(drawn.change_at(&plain, end) >= 6, "{what}: no glow in the last pixel");
    }
}
